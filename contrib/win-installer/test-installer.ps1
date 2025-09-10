#!/usr/bin/env pwsh

# Usage example:
#
# rm .\contrib\win-installer\*.log &&
# rm .\contrib\win-installer\*.exe &&
# rm .\contrib\win-installer\*.wixpdb &&
# .\winmake.ps1 installer &&
# .\winmake.ps1 installer 9.9.9 &&
# .\contrib\win-installer\test-installer.ps1 `
#     -scenario all `
#     -setupExePath ".\contrib\win-installer\podman-5.4.0-dev-setup.exe" `
#     -nextSetupExePath ".\contrib\win-installer\podman-9.9.9-dev-setup.exe" `
#     -provider hyperv
# .\contrib\win-installer\test-installer.ps1 `
#     -scenario installation-green-field `
#     -setupExePath ".\contrib\win-installer\podman-5.4.0-dev-setup.exe" `
#     -provider wsl

# The Param statement must be the first statement, except for comments and any #Require statements.
param (
    [Parameter(Mandatory)]
    [ValidateSet("test-objects-exist-per-user", "test-objects-exist-not-per-user",
                 "test-objects-exist-per-machine", "test-objects-exist-not-per-machine",
                 "installation-green-field", "installation-skip-config-creation-flag", "installation-with-pre-existing-podman-exe",
                 "update-without-user-changes", "update-with-user-changed-config-file", "update-with-user-removed-config-file",
                 "update-without-user-changes-to-next", "update-with-user-changed-config-file-to-next", "update-with-user-removed-config-file-to-next",
                 "update-without-user-changes-from-531", "update-without-user-changes-from-560",
                 "all")]
    [string]$scenario,
    [ValidateScript({Test-Path $_ -PathType Leaf})]
    [string]$setupExePath,
    [ValidateScript({Test-Path $_ -PathType Leaf})]
    [string]$previousSetupExePath,
    [ValidateScript({Test-Path $_ -PathType Leaf})]
    [string]$nextSetupExePath,
    [ValidateScript({Test-Path $_ -PathType Leaf})]
    [string]$v531SetupExePath,
    [ValidateScript({Test-Path $_ -PathType Leaf})]
    [string]$v560SetupExePath,
    [ValidateSet("wsl", "hyperv")]
    [string]$provider="wsl",
    [switch]$skipWinVersionCheck=$false,
    [switch]$skipConfigFileCreation=$false
)

. $PSScriptRoot\utils.ps1


$MachineConfPathPerMachine = "$env:ProgramData\containers\containers.conf.d\99-podman-machine-provider.conf"
$MachineConfPathPerUser = "$env:APPDATA\containers\containers.conf.d\99-podman-machine-provider.conf"
$PodmanFolderPathPerMachine = "$env:ProgramFiles\RedHat\Podman"
$PodmanFolderPathPerUser = "$env:LocalAppData\Programs\podman"
$PodmanExePathPerMachine = "$PodmanFolderPathPerMachine\podman.exe"
$PodmanExePathPerUser = "$PodmanFolderPathPerUser\podman.exe"
$WindowsPathsToTestPerMachine = @($PodmanExePathPerMachine,
"$PodmanFolderPathPerMachine\win-sshproxy.exe",
"HKLM:\SOFTWARE\Red Hat\Podman")
$WindowsPathsToTestPerUser = @($PodmanExePathPerUser,
"$PodmanFolderPathPerUser\win-sshproxy.exe",
"HKCU:\SOFTWARE\Podman")

function Confirm-Running-As-Admin {
    $currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    $isAdmin = $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    return $isAdmin
}

function Install-Podman {
    param (
        [Parameter(Mandatory)]
        [ValidateScript({Test-Path $_ -PathType Leaf})]
        [string]$setupExePath
    )
    if ($skipWinVersionCheck) {$allowOldWinVar = "1"} else {$allowOldWinVar = "0"}
    if ($skipConfigFileCreation) {$skipConfigFileCreationVar = "1"} else {$skipConfigFileCreationVar = "0"}

    Write-Host "Running the installer ($setupExePath)..."
    Write-Host "(provider=`"$provider`", AllowOldWin=`"$allowOldWinVar`", SkipConfigFileCreation=`"$skipConfigFileCreationVar`")"
    $ret = Start-Process -Wait `
                            -PassThru "$setupExePath" `
                            -ArgumentList "/install /quiet `
                                MachineProvider=${provider} `
                                AllowOldWin=${allowOldWinVar} `
                                SkipConfigFileCreation=${skipConfigFileCreationVar} `
                                /log $PSScriptRoot\podman-setup.log"
    if ($ret.ExitCode -ne 0) {
        Write-Host "Install failed, dumping log"
        Get-Content $PSScriptRoot\podman-setup.log
        throw "Exit code is $($ret.ExitCode)"
    }
    Write-Host "Installation completed successfully!`n"
}

# Install-Podman-With-Defaults is used to test updates. That's because when
# using the installer GUI the user can't change the default values.
function Install-Podman-With-Defaults {
    param (
        [Parameter(Mandatory)]
        [ValidateScript({Test-Path $_ -PathType Leaf})]
        [string]$setupExePath
    )

    Write-Host "Running the installer using defaults ($setupExePath)..."
    $ret = Start-Process -Wait `
                            -PassThru "$setupExePath" `
                            -ArgumentList "/install /quiet `
                                /log $PSScriptRoot\podman-setup-default.log"
    if ($ret.ExitCode -ne 0) {
        Write-Host "Install failed, dumping log"
        Get-Content $PSScriptRoot\podman-setup-default.log
        throw "Exit code is $($ret.ExitCode)"
    }
    Write-Host "Installation completed successfully!`n"
}

function Install-Podman-With-Defaults-Expected-Fail {
    param (
        [Parameter(Mandatory)]
        [ValidateScript({Test-Path $_ -PathType Leaf})]
        [string]$setupExePath
    )

    Write-Host "Running the installer using defaults ($setupExePath)..."
    $ret = Start-Process -Wait `
                            -PassThru "$setupExePath" `
                            -ArgumentList "/install /quiet `
                                /log $PSScriptRoot\podman-setup-default.log"
    if ($ret.ExitCode -eq 0) {
        Write-Host "Install completed successfully but a failure was expected, dumping log"
        Get-Content $PSScriptRoot\podman-setup-default.log
        throw "Exit code is $($ret.ExitCode)"
    }
    Write-Host "Installation has failed as expected!`n"
}

function Install-Current-Podman {
    Install-Podman -setupExePath $setupExePath
}

function Test-Podman-Objects-Exist-Per-Machine {
    Write-Host "Verifying that podman files, folders and registry entries exist...(per machine)"
    $WindowsPathsToTestPerMachine | ForEach-Object {
        if (! (Test-Path -Path $_) ) {
            throw "Expected $_ but doesn't exist"
        }
    }
    Write-Host "Verification was successful!`n"
}

function Test-Podman-Objects-Exist-Per-User {
    Write-Host "Verifying that podman files, folders and registry entries exist...(per user)"
    $WindowsPathsToTestPerUser | ForEach-Object {
        if (! (Test-Path -Path $_) ) {
            throw "Expected $_ but doesn't exist"
        }
    }
    Write-Host "Verification was successful!`n"
}

function Test-Podman-Machine-Conf-Exist-Per-Machine {
    Write-Host "Verifying that $MachineConfPathPerMachine exist..."
    if (! (Test-Path -Path $MachineConfPathPerMachine) ) {
        throw "Expected $MachineConfPathPerMachine but doesn't exist"
    }
    Write-Host "Verification was successful!`n"
}

function Test-Podman-Machine-Conf-Exist-Per-User {
    Write-Host "Verifying that $MachineConfPathPerUser exist..."
    if (! (Test-Path -Path $MachineConfPathPerUser) ) {
        throw "Expected $MachineConfPathPerUser but doesn't exist"
    }
    Write-Host "Verification was successful!`n"
}

function Test-Podman-Machine-Conf-Content-Per-User {
    [CmdletBinding(PositionalBinding=$false)]
    param (
        [ValidateSet("wsl", "hyperv")]
        [string]$expected=$provider
    )
    Write-Host "Verifying that the machine provider configuration is correct...(per user)"
    $machineProvider = Get-Content $MachineConfPathPerUser | Select-Object -Skip 1 | ConvertFrom-StringData | ForEach-Object { $_.provider }
    if ( $machineProvider -ne "`"$expected`"" ) {
        throw "Expected `"$expected`" as default machine provider but got $machineProvider"
    }
    Write-Host "Verification was successful!`n"
}

function Test-Podman-Machine-Conf-Content-Per-Machine {
    [CmdletBinding(PositionalBinding=$false)]
    param (
        [ValidateSet("wsl", "hyperv")]
        [string]$expected=$provider
    )
    Write-Host "Verifying that the machine provider configuration is correct...(per machine)"
    $machineProvider = Get-Content $MachineConfPathPerMachine | Select-Object -Skip 1 | ConvertFrom-StringData | ForEach-Object { $_.provider }
    if ( $machineProvider -ne "`"$expected`"" ) {
        throw "Expected `"$expected`" as default machine provider but got $machineProvider"
    }
    Write-Host "Verification was successful!`n"
}

function Uninstall-Podman {
    param (
        # [Parameter(Mandatory)]
        [ValidateScript({Test-Path $_ -PathType Leaf})]
        [string]$setupExePath
    )
    Write-Host "Running the uninstaller ($setupExePath)..."
    $ret = Start-Process -Wait `
                         -PassThru "$setupExePath" `
                         -ArgumentList "/uninstall `
                         /quiet /log $PSScriptRoot\podman-setup-uninstall.log"
    if ($ret.ExitCode -ne 0) {
        Write-Host "Uninstall failed, dumping log"
        Get-Content $PSScriptRoot\podman-setup-uninstall.log
        throw "Exit code is $($ret.ExitCode)"
    }
    Write-Host "The uninstallation completed successfully!`n"
}

function Uninstall-Current-Podman {
    Uninstall-Podman -setupExePath $setupExePath
}

function Test-Podman-Objects-Exist-Not-Per-User {
    Write-Host "Verifying that podman files, folders and registry entries don't exist...(per user)"
    $WindowsPathsToTestPerUser | ForEach-Object {
        if ( Test-Path -Path $_ ) {
            throw "Path $_ is present"
        }
    }
    Write-Host "Verification was successful!`n"
}

function Test-Podman-Objects-Exist-Not-Per-Machine {
    Write-Host "Verifying that podman files, folders and registry entries don't exist...(per machine)"
    $WindowsPathsToTestPerMachine | ForEach-Object {
        # HKLM:\SOFTWARE\Red Hat\Podman is expected to exist after upgrade
        if ($_ -eq "HKLM:\SOFTWARE\Red Hat\Podman") {
        } elseif ( Test-Path -Path $_ ) {
            throw "Path $_ is present"
        }
    }
    Write-Host "Verification was successful!`n"
}

function Test-Podman-Machine-Conf-Exist-Not-Per-User {
    Write-Host "Verifying that $MachineConfPathPerUser doesn't exist..."
    if ( Test-Path -Path $MachineConfPathPerUser ) {
        throw "Path $MachineConfPathPerUser is present"
    }
    Write-Host "Verification was successful!`n"
}

function Test-Podman-Machine-Conf-Exist-Not-Per-Machine {
    Write-Host "Verifying that $MachineConfPathPerMachine doesn't exist..."
    if ( Test-Path -Path $MachineConfPathPerMachine ) {
        throw "Path $MachineConfPathPerMachine is present"
    }
    Write-Host "Verification was successful!`n"
}

function New-Fake-Podman-Exe {
    Write-Host "Creating a fake $PodmanExePathPerUser..."
    New-Item -ItemType Directory -Path $PodmanFolderPathPerUser -Force -ErrorAction Stop | out-null
    New-Item -ItemType File -Path $PodmanExePathPerUser -ErrorAction Stop | out-null
    Write-Host "Creation successful!`n"
}

function Switch-Podman-Machine-Conf-Content {
    $currentProvider = $provider
    if ( $currentProvider -eq "wsl" ) { $newProvider = "hyperv" } else { $newProvider = "wsl" }
    Write-Host "Editing $MachineConfPathPerUser content (was $currentProvider, will be $newProvider)..."
    "[machine]`nprovider=`"$newProvider`"" | Out-File -FilePath $MachineConfPathPerUser -ErrorAction Stop
    Write-Host "Edit successful!`n"
    return $newProvider
}

function Remove-Podman-Machine-Conf {
    Write-Host "Deleting $MachineConfPathPerUser..."
    Remove-Item -Path $MachineConfPathPerUser -ErrorAction Stop | out-null
    Write-Host "Deletion successful!`n"
}

function Test-Installation {
    [CmdletBinding(PositionalBinding=$false)]
    param (
        [ValidateSet("wsl", "hyperv")]
        [string]$expectedConf
    )

    Test-Podman-Objects-Exist-Per-User
    Test-Podman-Machine-Conf-Exist-Per-User
    Test-Podman-Objects-Exist-Not-Per-Machine
    Test-Podman-Machine-Conf-Exist-Not-Per-Machine

    if ($expectedConf) {
        Test-Podman-Machine-Conf-Content-Per-User -expected $expectedConf
    } else {
        Test-Podman-Machine-Conf-Content-Per-User
    }
}

function Test-Installation-Per-Machine {
    [CmdletBinding(PositionalBinding=$false)]
    param (
        [ValidateSet("wsl", "hyperv")]
        [string]$expectedConf
    )

    Test-Podman-Objects-Exist-Per-Machine
    Test-Podman-Machine-Conf-Exist-Per-Machine
    Test-Podman-Objects-Exist-Not-Per-User
    Test-Podman-Machine-Conf-Exist-Not-Per-User

    if ($expectedConf) {
        Test-Podman-Machine-Conf-Content-Per-Machine -expected $expectedConf
    } else {
        Test-Podman-Machine-Conf-Content-Per-Machine
    }
}

function Test-Installation-No-Config {
    Test-Podman-Objects-Exist-Per-User
    Test-Podman-Machine-Conf-Exist-Not-Per-User
}

function Test-Uninstallation {
    Test-Podman-Objects-Exist-Not-Per-User
    Test-Podman-Machine-Conf-Exist-Not-Per-User
}

function Test-Uninstallation-Per-Machine {
    Test-Podman-Objects-Exist-Not-Per-Machine
    # After upgrade the machine scope podman conf is expected to exist
    # Test-Podman-Machine-Conf-Exist-Not-Per-Machine
}

# SCENARIOS
function Start-Scenario-Installation-Green-Field {
    Write-Host "`n==========================================="
    Write-Host " Running scenario: Installation-Green-Field"
    Write-Host "==========================================="
    Install-Current-Podman
    Test-Installation
    Uninstall-Current-Podman
    Test-Uninstallation
}

function Start-Scenario-Installation-Skip-Config-Creation-Flag {
    Write-Host "`n========================================================="
    Write-Host " Running scenario: Installation-Skip-Config-Creation-Flag"
    Write-Host "========================================================="
    $skipConfigFileCreation = $true
    Install-Current-Podman
    Test-Installation-No-Config
    Uninstall-Current-Podman
    Test-Uninstallation
}

function Start-Scenario-Installation-With-Pre-Existing-Podman-Exe {
    Write-Host "`n============================================================"
    Write-Host " Running scenario: Installation-With-Pre-Existing-Podman-Exe"
    Write-Host "============================================================"
    New-Fake-Podman-Exe
    Install-Current-Podman
    Test-Installation-No-Config
    Uninstall-Current-Podman
    Test-Uninstallation
}

function Start-Scenario-Update-Without-User-Changes {
    param (
        [ValidateSet("From-Previous", "To-Next", "From-v531", "From-v560")]
        [string]$mode="From-Previous"
    )
    Write-Host "`n======================================================"
    Write-Host " Running scenario: Update-Without-User-Changes-$mode"
    Write-Host "======================================================"
    switch ($mode) {
        'From-Previous' {$i = $previousSetupExePath; $u = $setupExePath}
        'To-Next' {$i = $setupExePath; $u = $nextSetupExePath}
        'From-v531' {$i = $v531SetupExePath; $u = $setupExePath}
        'From-v560' {$i = $v560SetupExePath; $u = $setupExePath}
    }

    if ($mode -eq "From-v560" -or $mode -eq "From-v531" -or $mode -eq "From-Previous") { # Previous installers have scope "per-machine"
        if (-not (Confirm-Running-As-Admin)) {
            throw "This tests requires Administrator privileges. Please run the terminal as an Administrator and try again."
        }
        Install-Podman -setupExePath $i
        Test-Installation-Per-Machine
    } else {
        Install-Podman -setupExePath $i
        Test-Installation
    }

    # Updates are expected to succeed except when updating from v5.3.1
    # The v5.3.1 installer has a bug that is patched in v5.3.2
    # Upgrading from v5.3.1 requires upgrading to v5.3.2 first
    if ($mode -eq "From-Previous" -or $mode -eq "To-Next") {
        Install-Podman-With-Defaults -setupExePath $u
        Test-Installation
        Uninstall-Podman -setupExePath $u
    } elseif ($mode -eq "From-v560") { # v5.6.0 is the last installer with scope "per-machine"
        Install-Podman-With-Defaults -setupExePath $u
        Test-Installation
        Test-Uninstallation-Per-Machine
        Uninstall-Podman -setupExePath $u
    } else { # From-v531 is expected to fail
        Install-Podman-With-Defaults-Expected-Fail -setupExePath $u
        Uninstall-Podman -setupExePath $i
    }
    Test-Uninstallation
}

function Start-Scenario-Update-Without-User-Changes-To-Next {
    Start-Scenario-Update-Without-User-Changes -mode "To-Next"
}

function Start-Scenario-Update-Without-User-Changes-From-v531 {
    Start-Scenario-Update-Without-User-Changes -mode "From-v531"
}

function Start-Scenario-Update-Without-User-Changes-From-v560 {
    Start-Scenario-Update-Without-User-Changes -mode "From-v560"
}

function Start-Scenario-Update-With-User-Changed-Config-File {
    param (
        [ValidateSet("From-Previous", "To-Next")]
        [string]$mode="From-Previous"
    )
    Write-Host "`n=============================================================="
    Write-Host " Running scenario: Update-With-User-Changed-Config-File-$mode"
    Write-Host "=============================================================="
    switch ($mode) {
        'From-Previous' {$i = $previousSetupExePath; $u = $setupExePath}
        'To-Next' {$i = $setupExePath; $u = $nextSetupExePath}
    }
    Install-Podman -setupExePath $i
    Test-Installation
    $newProvider = Switch-Podman-Machine-Conf-Content
    Install-Podman-With-Defaults -setupExePath $u
    Test-Installation -expectedConf $newProvider
    Uninstall-Podman -setupExePath $u
    Test-Uninstallation
}

function Start-Scenario-Update-With-User-Changed-Config-File-To-Next {
    Start-Scenario-Update-With-User-Changed-Config-File -mode "To-Next"
}

function Start-Scenario-Update-With-User-Removed-Config-File {
    param (
        [ValidateSet("From-Previous", "To-Next")]
        [string]$mode="From-Previous"
    )
    Write-Host "`n=============================================================="
    Write-Host " Running scenario: Update-With-User-Removed-Config-File-$mode"
    Write-Host "=============================================================="
    switch ($mode) {
        'From-Previous' {$i = $previousSetupExePath; $u = $setupExePath}
        'To-Next' {$i = $setupExePath; $u = $nextSetupExePath}
    }
    Install-Podman -setupExePath $i
    Test-Installation
    Remove-Podman-Machine-Conf
    Install-Podman-With-Defaults -setupExePath $u
    Test-Installation-No-Config
    Uninstall-Podman -setupExePath $u
    Test-Uninstallation
}

function Start-Scenario-Update-With-User-Removed-Config-File-To-Next {
    Start-Scenario-Update-With-User-Removed-Config-File -mode "To-Next"
}

switch ($scenario) {
    'test-objects-exist-per-user' {
        Test-Podman-Objects-Exist-Per-User
    }
    'test-objects-exist-not-per-user' {
        Test-Podman-Objects-Exist-Not-Per-User
    }
    'test-objects-exist-per-machine' {
        Test-Podman-Objects-Exist-Per-Machine
    }
    'test-objects-exist-not-per-machine' {
        Test-Podman-Objects-Exist-Not-Per-Machine
    }
    'installation-green-field' {
        Start-Scenario-Installation-Green-Field
    }
    'installation-skip-config-creation-flag' {
        Start-Scenario-Installation-Skip-Config-Creation-Flag
    }
    'installation-with-pre-existing-podman-exe' {
        Start-Scenario-Installation-With-Pre-Existing-Podman-Exe
    }
    'update-without-user-changes' {
        if (!$previousSetupExePath) {
            $previousSetupExePath = Get-Latest-Podman-Setup-From-GitHub
        }
        Start-Scenario-Update-Without-User-Changes
    }
    'update-without-user-changes-to-next' {
        if (!$nextSetupExePath) {
            throw "Next version installer path is not defined. Use '-nextSetupExePath <setup-exe-path>' to define it."
        }
        Start-Scenario-Update-Without-User-Changes-To-Next
    }
    'update-without-user-changes-from-531' {
        if (!$v531SetupExePath) {
            $v531SetupExePath = Get-Podman-Setup-From-GitHub -version "tags/v5.3.1"
        }
        Start-Scenario-Update-Without-User-Changes-From-v531
    }
    'update-without-user-changes-from-560' {
        if (!$v560SetupExePath) {
            $v560SetupExePath = Get-Podman-Setup-From-GitHub -version "tags/v5.6.0"
        }
        Start-Scenario-Update-Without-User-Changes-From-v560
    }
    'update-with-user-changed-config-file' {
        if (!$previousSetupExePath) {
            $previousSetupExePath = Get-Latest-Podman-Setup-From-GitHub
        }
        Start-Scenario-Update-With-User-Changed-Config-File
    }
    'update-with-user-changed-config-file-to-next' {
        if (!$nextSetupExePath) {
            throw "Next version installer path is not defined. Use '-nextSetupExePath <setup-exe-path>' to define it."
        }
        Start-Scenario-Update-With-User-Changed-Config-File-To-Next
    }
    'update-with-user-removed-config-file' {
        if (!$previousSetupExePath) {
            $previousSetupExePath = Get-Latest-Podman-Setup-From-GitHub
        }
        Start-Scenario-Update-With-User-Removed-Config-File
    }
    'update-with-user-removed-config-file-to-next' {
        if (!$nextSetupExePath) {
            throw "Next version installer path is not defined. Use '-nextSetupExePath <setup-exe-path>' to define it."
        }
        Start-Scenario-Update-With-User-Removed-Config-File-To-Next
    }
    'all' {
        if (!$nextSetupExePath) {
            throw "Next version installer path is not defined. Use '-nextSetupExePath <setup-exe-path>' to define it."
        }
        # if (!$previousSetupExePath) {
        #     $previousSetupExePath = Get-Latest-Podman-Setup-From-GitHub
        # }
        Start-Scenario-Installation-Green-Field
        Start-Scenario-Installation-Skip-Config-Creation-Flag
        Start-Scenario-Installation-With-Pre-Existing-Podman-Exe
        # Start-Scenario-Update-Without-User-Changes
        Start-Scenario-Update-Without-User-Changes-To-Next
        # Start-Scenario-Update-With-User-Changed-Config-File
        Start-Scenario-Update-With-User-Changed-Config-File-To-Next
        # Start-Scenario-Update-With-User-Removed-Config-File
        Start-Scenario-Update-With-User-Removed-Config-File-To-Next

        if (Confirm-Running-As-Admin) {
            if (!$v531SetupExePath) {
                $v531SetupExePath = Get-Podman-Setup-From-GitHub -version "tags/v5.3.1"
            }
            Start-Scenario-Update-Without-User-Changes-From-v531

            if (!$v560SetupExePath) {
                $v560SetupExePath = Get-Podman-Setup-From-GitHub -version "tags/v5.6.0"
            }
            Start-Scenario-Update-Without-User-Changes-From-v560
        } else {
             Write-Warning "Scenarios ""update-without-user-changes-from-531"" and ""update-without-user-changes-from-560"" requires Administrator privileges."
        }
    }
}
