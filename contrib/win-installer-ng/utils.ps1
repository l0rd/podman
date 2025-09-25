#!/usr/bin/env pwsh
function Get-Latest-Podman-Setup-From-GitHub {
    param(
        [ValidateSet("amd64", "arm64")]
        [string] $arch = "amd64"
    )
    return Get-Podman-Setup-From-GitHub "latest" $arch
}

function Get-Podman-Setup-From-GitHub {
    param(
        [Parameter(Mandatory)]
        [string] $version,
        [ValidateSet("amd64", "arm64")]
        [string] $arch = "amd64"
    )

    Write-Host "Downloading the $arch $version Podman windows setup from GitHub..."
    $apiUrl = "https://api.github.com/repos/containers/podman/releases/$version"
    $response = Invoke-RestMethod -Uri $apiUrl -Headers @{"User-Agent"="PowerShell"} -ErrorAction Stop
    $latestTag = $response.tag_name
    Write-Host "Looking for an asset named ""podman-installer-windows-$arch.exe"""
    $downloadAsset = $response.assets | Where-Object { $_.name -eq "podman-installer-windows-$arch.exe" } | Select-Object -First 1
    if (-not $downloadAsset) {
        # remove the first char from $latestTag if it is a "v"
        if ($latestTag[0] -eq "v") {
            $newLatestTag = $latestTag.Substring(1)
        }
        Write-Host "Not found. Looking for an asset named ""podman-$newLatestTag-setup.exe"""
        $downloadAsset = $response.assets | Where-Object { $_.name -eq "podman-$newLatestTag-setup.exe" } | Select-Object -First 1
    }
    $downloadUrl = $downloadAsset.browser_download_url
    Write-Host "Downloading URL: $downloadUrl"
    $destinationPath = "$PSScriptRoot\podman-${latestTag}-setup.exe"
    Write-Host "Destination Path: $destinationPath"
    Invoke-WebRequest -Uri $downloadUrl -OutFile $destinationPath
    Write-Host "Command completed successfully!`n"
    return $destinationPath
}
