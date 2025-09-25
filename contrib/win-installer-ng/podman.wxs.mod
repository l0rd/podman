<Wix xmlns="http://wixtoolset.org/schemas/v4/wxs" xmlns:PanelSW="http://schemas.panel-sw.co.il/wix/WixExtension" xmlns:util="http://wixtoolset.org/schemas/v4/wxs/util">

	<?ifndef VERSION?>
	<?error VERSION must be defined via command line argument?>
	<?endif?>

	<?ifdef env.UseGVProxy?>
	<?define UseGVProxy = "$(env.UseGVProxy)"?>
	<?else?>
	<?define UseGVProxy = ""?>
	<?endif?>

	<?define ApplicationFolderName = "podman" ?>

	<Package Name="podman" Manufacturer="Red Hat Inc." Version="$(VERSION)" UpgradeCode="a6a9dd9c-0732-44ba-9279-ffe22ea50671" Scope="perUserOrMachine">
		<Media Id="1" Cabinet="Podman.cab" EmbedCab="yes" />
		<MajorUpgrade DowngradeErrorMessage="A newer version of [ProductName] is already installed." RemoveFeatures="Complete" Schedule="afterInstallExecute" />

		<!-- Explicit upgrade detection for cross-scope scenarios -->
		<Upgrade Id="a6a9dd9c-0732-44ba-9279-ffe22ea50671">
			<UpgradeVersion Maximum="$(VERSION)" IncludeMaximum="no" Property="PREVIOUSVERSIONSINSTALLED" />
			<UpgradeVersion Minimum="$(VERSION)" OnlyDetect="yes" Property="NEWERVERSIONDETECTED" />
		</Upgrade>
		<Property Id="DiskPrompt" Value="Red Hat's Podman $(VERSION) Installation" />
		<Property Id="MACHINE_PROVIDER" Value="wsl" />

		<!-- Legacy installation detection - search for previous installations in all possible locations -->
		<!-- Legacy Red Hat per-machine installation -->
		<Property Id="LEGACY_REDHAT_INSTALL_DIR">
			<RegistrySearch Id="LegacyRedHatInstallDirSearch" Root="HKLM" Key="SOFTWARE\Red Hat\Podman" Name="InstallDir" Type="raw" />
		</Property>
		<!-- <Property Id="LEGACY_REDHAT_PODMAN_EXE">
			<DirectorySearch Id="LegacyRedHatPodmanSearch" Path="[ProgramFiles64Folder]RedHat\Podman">
				<FileSearch Name="podman.exe" />
			</DirectorySearch>
		</Property> -->

		<!-- Current per-machine installation -->
		<Property Id="CURRENT_MACHINE_INSTALL_DIR">
			<RegistrySearch Id="CurrentMachineInstallDirSearch" Root="HKLM" Key="SOFTWARE\Podman" Name="InstallDir" Type="raw" />
		</Property>
		<!-- <Property Id="CURRENT_MACHINE_PODMAN_EXE">
			<DirectorySearch Id="CurrentMachinePodmanSearch" Path="[ProgramFiles64Folder]podman">
				<FileSearch Name="podman.exe" />
			</DirectorySearch>
		</Property> -->

		<!-- Current per-user installation -->
		<Property Id="CURRENT_USER_INSTALL_DIR">
			<RegistrySearch Id="CurrentUserInstallDirSearch" Root="HKCU" Key="SOFTWARE\Podman" Name="InstallDir" Type="raw" />
		</Property>
		<Property Id="CURRENT_USER_PODMAN_EXE">
			<DirectorySearch Id="CurrentUserPodmanSearch" Path="[LocalAppDataFolder]Programs\podman">
				<FileSearch Name="podman.exe" />
			</DirectorySearch>
		</Property>

		<!-- Determine if this is an upgrade and from which type of installation -->
		<SetProperty Id="IS_UPGRADE" After="AppSearch" Value="1" Sequence="first"
			Condition="LEGACY_REDHAT_INSTALL_DIR OR CURRENT_MACHINE_INSTALL_DIR OR CURRENT_USER_INSTALL_DIR OR CURRENT_USER_PODMAN_EXE" />
		<SetProperty Id="IS_UPGRADE_FROM_LEGACY" After="AppSearch" Value="1" Sequence="first"
			Condition="LEGACY_REDHAT_INSTALL_DIR" />
		<SetProperty Id="IS_UPGRADE_FROM_MACHINE" After="AppSearch" Value="1" Sequence="first"
			Condition="CURRENT_MACHINE_INSTALL_DIR" />
		<SetProperty Id="IS_UPGRADE_FROM_USER" After="AppSearch" Value="1" Sequence="first"
			Condition="CURRENT_USER_INSTALL_DIR OR CURRENT_USER_PODMAN_EXE" />

		<!-- Allow install for Current User Or Machine -->
		<Property Id="WixUISupportPerUser" Value="1" />
		<Property Id="WixUISupportPerMachine" Value="1" />

		<!-- Install for PerUser by default -->
		<!-- If set to WixPerMachineFolder will install for PerMachine by default -->
		<Property Id="WixAppFolder" Value="WixLocalAppDataFolder" />

		<!-- Workaround Wix Bug: https://github.com/wixtoolset/issues/issues/2165 -->
		<!-- The suggested folder in the dest folder dialog should be C:\Program Files\nu -->
		<!-- <CustomAction Id="Overwrite_WixSetDefaultPerMachineFolder" Property="WixPerMachineFolder"
			Value="[ProgramFiles64Folder][ApplicationFolderName]" Execute="immediate" />
		<CustomAction Id="Overwrite_ARPINSTALLLOCATION" Property="ARPINSTALLLOCATION"
			Value="[ProgramFiles64Folder][ApplicationFolderName]" Execute="immediate" />
		<InstallUISequence>
			<Custom Action="Overwrite_WixSetDefaultPerMachineFolder" After="WixSetDefaultPerMachineFolder" />
		</InstallUISequence>
		<InstallExecuteSequence>
			<Custom Action="Overwrite_WixSetDefaultPerMachineFolder" After="WixSetDefaultPerMachineFolder" />
			<Custom Action="Overwrite_ARPINSTALLLOCATION" After="InstallValidate"/>
		</InstallExecuteSequence> -->

		<!-- If installing per-user (MSIINSTALLPERUSER=1), this sets INSTALLDIR and CONFDIR to point to user-specific paths -->
		<SetProperty Id="INSTALLDIR"
				Action="SetINSTALLDIR_User"
				Value="[LocalAppDataFolder]Programs\$(var.ApplicationFolderName)"
				After="LaunchConditions"
				Condition="MSIINSTALLPERUSER=1 AND NOT IS_UPGRADE"
				Sequence="both" />
		<SetProperty Id="CONFDIR"
				Action="SetCONFDIR_User"
				Value="[AppDataFolder]containers\containers.conf.d"
				After="LaunchConditions"
				Condition="MSIINSTALLPERUSER=1 AND NOT IS_UPGRADE"
				Sequence="both" />
		<SetProperty Id="WixUnelevatedShellExecTarget"
				Action="SetWixUnelevatedShellExecTarget_User"
				Value="[#GuideHTMLFile_USER]"
				After="ExecuteAction"
				Condition="MSIINSTALLPERUSER=1 AND NOT IS_UPGRADE"
				Sequence="ui" />

		<!-- If installing per-machine (ALLUSERS=1 AND NOT MSIINSTALLPERUSER=1), this sets INSTALLDIR and CONFDIR to point to machine-wide (Program Files) paths -->
		<SetProperty Id="INSTALLDIR"
				Action="SetINSTALLDIR_Machine"
				Value="[ProgramFiles64Folder]$(var.ApplicationFolderName)"
				After="LaunchConditions"
				Condition="ALLUSERS=1 AND NOT MSIINSTALLPERUSER=1 AND NOT IS_UPGRADE"
				Sequence="both" />
		<SetProperty Id="CONFDIR"
				Action="SetCONFDIR_Machine"
				Value="[CommonAppDataFolder]containers\containers.conf.d"
				After="LaunchConditions"
				Condition="ALLUSERS=1 AND NOT MSIINSTALLPERUSER=1 AND NOT IS_UPGRADE"
				Sequence="both" />
		<SetProperty Id="WixUnelevatedShellExecTarget"
				Action="SetWixUnelevatedShellExecTarget_Machine"
				Value="[#GuideHTMLFile_MACHINE]"
				After="ExecuteAction"
				Condition="ALLUSERS=1 AND NOT MSIINSTALLPERUSER=1 AND NOT IS_UPGRADE"
				Sequence="ui" />

		<!-- <SetProperty Id="INSTALLDIR"
				Action="SetINSTALLDIR_Legacy"
				Value="[ProgramFiles64Folder]RedHat\$(var.ApplicationFolderName)"
				After="LaunchConditions"
				Condition="IS_UPGRADE_FROM_LEGACY"
				Sequence="both" />
		<SetProperty Id="CONFDIR"
				Action="SetCONFDIR_Legacy"
				Value="[CommonAppDataFolder]containers\containers.conf.d"
				After="LaunchConditions"
				Condition="IS_UPGRADE_FROM_LEGACY"
				Sequence="both" />
		<SetProperty Id="WixUnelevatedShellExecTarget"
				Action="SetWixUnelevatedShellExecTarget_Legacy"
				Value="[#GuideHTMLFile_MACHINE]"
				After="ExecuteAction"
				Condition="IS_UPGRADE_FROM_LEGACY"
				Sequence="ui" /> -->

		<Property Id="MACHINE_PROVIDER_CONFIG_FILE_PATH">
			<DirectorySearch Id="CONFDIRFolderSearch" Path="[CONFDIR]">
				<FileSearch Name="99-podman-machine-provider.conf" />
			</DirectorySearch>
		</Property>
		<Property Id="MAIN_EXECUTABLE_FILE_PATH">
			<DirectorySearch Id="INSTALLDIRFolderSearch" Path="[INSTALLDIR]">
				<FileSearch Name="podman.exe" />
			</DirectorySearch>
		</Property>

		<!-- Also search for config files in legacy locations -->
		<Property Id="LEGACY_MACHINE_PROVIDER_CONFIG_FILE_PATH">
			<DirectorySearch Id="LegacyMachineConfSearch" Path="[CommonAppDataFolder]containers\containers.conf.d">
				<FileSearch Name="99-podman-machine-provider.conf" />
			</DirectorySearch>
		</Property>
		<Property Id="LEGACY_USER_MACHINE_PROVIDER_CONFIG_FILE_PATH">
			<DirectorySearch Id="LegacyUserConfSearch" Path="[AppDataFolder]containers\containers.conf.d">
				<FileSearch Name="99-podman-machine-provider.conf" />
			</DirectorySearch>
		</Property>
		<!--
		Property CREATE_MACHINE_PROVIDER_CONFIG_FILE is set at runtime and used as the condition to run the `MachineProviderConfigFile` Component:
		The machine provider config file is created (or is not deleted if it already exist) if these conditions are met:
			- The user hasn't set property `SKIP_CONFIG_FILE_CREATION` to 1
			- No existing installation is found, OR if an existing installation is found, a config file already exists
		-->
		<SetProperty Id="CREATE_MACHINE_PROVIDER_CONFIG_FILE" After="AppSearch" Value="1" Sequence="first"
			Condition="(NOT (SKIP_CONFIG_FILE_CREATION = 1)) AND ((NOT IS_UPGRADE) OR (MACHINE_PROVIDER_CONFIG_FILE_PATH) OR (LEGACY_MACHINE_PROVIDER_CONFIG_FILE_PATH) OR (LEGACY_USER_MACHINE_PROVIDER_CONFIG_FILE_PATH))" />
		<!--
		Property HIDE_PROVIDER_CHOICE is set at runtime and used as the condition to hide the Machine Provider
		choice from the MSI GUI (the Radio Button Group and other related controls):
		The machine provider choice isn't shown to the user if one of these conditions are met:
			- The user has set the property `SKIP_CONFIG_FILE_CREATION` to 1
			- Any machine provider config file exists (current or legacy locations)
			- Any existing Podman installation is found (current or legacy locations)
		-->
		<SetProperty Id="HIDE_PROVIDER_CHOICE" After="AppSearch" Value="1" Sequence="first"
			Condition="(SKIP_CONFIG_FILE_CREATION = 1) OR (MACHINE_PROVIDER_CONFIG_FILE_PATH) OR (LEGACY_MACHINE_PROVIDER_CONFIG_FILE_PATH) OR (LEGACY_USER_MACHINE_PROVIDER_CONFIG_FILE_PATH) OR (IS_UPGRADE)" />

		<!-- Block installation if legacy per-machine installation exists and we're installing per-user -->
		<Launch Condition="NOT (MSIINSTALLPERUSER=1 AND (IS_UPGRADE_FROM_LEGACY OR IS_UPGRADE_FROM_MACHINE))"
			Message="A previous version of Podman is installed in Program Files. Please uninstall it first using 'Add or Remove Programs' before installing this version." />

		<!-- Block installation if per-user installation exists and we're installing per-machine -->
		<Launch Condition="NOT (ALLUSERS=1 AND IS_UPGRADE_FROM_USER)"
			Message="A previous version of Podman is installed for the current user. Please uninstall it first before installing this version for all users." />

		<!-- ALTERNATIVE: Automatic cross-scope upgrade (comment out the conditions above to enable this) -->
		<!--
		<CustomAction Id="UninstallLegacyInstallation" Directory="SystemFolder"
			ExeCommand='msiexec.exe /x {73752F94-6589-4C7B-ABED-39D655A19714} /quiet /norestart'
			Execute="immediate" Return="ignore"
			Condition="IS_UPGRADE_FROM_LEGACY OR IS_UPGRADE_FROM_MACHINE" />

		<InstallExecuteSequence>
			<Custom Action="UninstallLegacyInstallation" Before="InstallValidate">
				IS_UPGRADE_FROM_LEGACY OR IS_UPGRADE_FROM_MACHINE
			</Custom>
		</InstallExecuteSequence>
		-->

		<CustomAction Id="OpenGuide" DllEntry="WixUnelevatedShellExec" BinaryRef="Wix4UtilCA_$(sys.BUILDARCHSHORT)" Execute="immediate" Return="check"/>
		<util:BroadcastEnvironmentChange />
		<Feature Id="Complete" Level="1">
			<ComponentRef Id="INSTALLDIR_Component_MACHINE" />
			<ComponentRef Id="INSTALLDIR_Component_USER" />
			<ComponentRef Id="EnvEntriesComponent_MACHINE" />
			<ComponentRef Id="MainExecutable_MACHINE" />
			<ComponentRef Id="MainExecutable_USER" />
			<ComponentRef Id="WinSshProxyExecutable_MACHINE" />
			<ComponentRef Id="WinSshProxyExecutable_USER" />
			<?if $(var.UseGVProxy) != Skip?>
			<ComponentRef Id="GvProxyExecutable_MACHINE" />
			<ComponentRef Id="GvProxyExecutable_USER" />
			<?endif?>
			<ComponentRef Id="GuideHTMLComponent_MACHINE" />
			<ComponentRef Id="GuideHTMLComponent_USER" />
			<ComponentGroupRef Id="ManFiles" />
		</Feature>
		<Feature Id="MachineProviderConfig" Level="1">
			<ComponentRef Id="MachineProviderConfigFileUser" />
			<ComponentRef Id="MachineProviderConfigFileMachine" />
		</Feature>

		<Icon Id="podman.ico" SourceFile="resources/podman-logo.ico" />
		<Property Id="ApplicationFolderName" Value="$(var.ApplicationFolderName)" />
		<Property Id="ARPPRODUCTICON" Value="podman.ico" />
		<Property Id="WixShellExecTarget" Value="[#GuideHTMLFile]" />
		<Property Id="WIXUI_EXITDIALOGOPTIONALCHECKBOXTEXT" Value="Show Getting Started Guide" />
		<Property Id="WIXUI_EXITDIALOGOPTIONALCHECKBOX" Value="1" />
		<WixVariable Id="WixUIBannerBmp" Value="resources\podman-banner.png" />
		<WixVariable Id="WixUIDialogBmp" Value="resources\podman-dialog.png" />
		<UIRef Id="PodmanUI" />
		<UI>
			<Publish Dialog="ExitDialog" Control="Finish" Event="DoAction" Value="OpenGuide" Condition="(WIXUI_EXITDIALOGOPTIONALCHECKBOX = 1) AND (NOT Installed) AND (NOT UpdateStarted)" />
		</UI>

		<!-- INSTALLDIR is the logical target directory whose path will be set by the INSTALLDIR property. -->
		<Directory Id="INSTALLDIR" Name="$(var.ApplicationFolderName)"></Directory>
		<!-- CONFDIR is the logical target directory whose path will be set by the CONFDIR property. -->
		<Directory Id="CONFDIR" Name="$(var.ApplicationFolderName)Conf"></Directory>

		<!-- Per Machine Install - these are the definitions of the physical locations -->
		<StandardDirectory Id="ProgramFiles64Folder">
			<Directory Id="APPLICATIONFOLDER" Name="$(var.ApplicationFolderName)">
				<Component Id="INSTALLDIR_Component_MACHINE" Guid="14B310C4-9B5D-4DA5-ADF9-B9D008E4CD82" Bitness="always64" Condition="ALLUSERS=1 AND NOT MSIINSTALLPERUSER=1">
					<CreateFolder />
					<RegistryKey Root="HKMU" Key="SOFTWARE\Podman">
						<RegistryValue Name="InstallDir" Value="[INSTALLDIR]" Type="string" />
					</RegistryKey>
				</Component>
				<Component Id="MainExecutable_MACHINE" Guid="73752F94-6589-4C7B-ABED-39D655A19714" Bitness="always64" Condition="ALLUSERS=1 AND NOT MSIINSTALLPERUSER=1">
					<File Id="MainExecutableFile_MACHINE" Name="podman.exe" Source="artifacts/podman.exe" KeyPath="yes" />
				</Component>
				<Component Id="WinSshProxyExecutable_MACHINE" Guid="0DA730AB-2F97-40E8-A8FC-356E88EAA4D2" Bitness="always64" Condition="ALLUSERS=1 AND NOT MSIINSTALLPERUSER=1">
					<File Id="WinSshProxyExecutableFile_MACHINE" Name="win-sshproxy.exe" Source="artifacts/win-sshproxy.exe" KeyPath="yes" />
				</Component>
				<?if $(var.UseGVProxy) != Skip?>
				<Component Id="GvProxyExecutable_MACHINE" Guid="1A4A2975-AD2D-44AA-974B-9B343C098333" Bitness="always64" Condition="ALLUSERS=1 AND NOT MSIINSTALLPERUSER=1">
					<File Id="GvProxyExecutableFile_MACHINE" Name="gvproxy.exe" Source="artifacts/gvproxy.exe" KeyPath="yes" />
				</Component>
				<?endif?>
				<Component Id="GuideHTMLComponent_MACHINE" Guid="8B23C76B-F7D4-4030-8C46-1B5729E616B5" Bitness="always64" Condition="ALLUSERS=1 AND NOT MSIINSTALLPERUSER=1">
					<File Id="GuideHTMLFile_MACHINE" Name="welcome-podman.html" Source="docs/podman-for-windows.html" KeyPath="yes" />
				</Component>
			</Directory>
		</StandardDirectory>
		<StandardDirectory Id="CommonFiles64Folder"> <!-- CommonAppDataFolder -->
			<Directory Id="CommonAppContainersFolder" Name="containers">
				<Directory Id="ContainersConfigSubDirMachine" Name="containers.conf.d">
					<Component Id="MachineProviderConfigFileMachine" Guid="C32C0040-D9AF-4155-AC7E-465B63B6BE3B" Condition="ALLUSERS=1 AND NOT MSIINSTALLPERUSER=1 AND CREATE_MACHINE_PROVIDER_CONFIG_FILE" Transitive="true">
						<CreateFolder />
						<IniFile Id="MachineProviderConfigFileMachine" Action="createLine" Directory="ContainersConfigSubDirMachine" Section="machine" Name="99-podman-machine-provider.conf" Key="provider" Value="&quot;[MACHINE_PROVIDER]&quot;" />
					</Component>
				</Directory>
			</Directory>
		</StandardDirectory>
		<Directory Id="EnvEntries_MACHINE">
			<Component Id="EnvEntriesComponent_MACHINE" Guid="b662ec43-0e0e-4018-8bf3-061904bb8f5b" Bitness="always64" Condition="ALLUSERS=1 AND NOT MSIINSTALLPERUSER=1">
				<CreateFolder />
				<Environment Id="UpdatePath_MACHINE" Name="PATH" Action="set" Permanent="no" System="yes" Part="last" Value="[INSTALLDIR]" />
			</Component>
		</Directory>

		<!-- Install for Current User - these are the definitions of the physical locations -->
		<StandardDirectory Id="LocalAppDataFolder">
			<Directory Id="LocalAppProgramsFolder" Name="Programs">
				<Directory Id="INSTALLDIR_USER" Name="$(var.ApplicationFolderName)">
					<Component Id="INSTALLDIR_Component_USER" Guid="*" Bitness="always64" Condition="MSIINSTALLPERUSER=1 AND NOT IS_UPGRADE">
						<CreateFolder />
						<RegistryKey Root="HKMU" Key="SOFTWARE\Podman">
							<RegistryValue Name="InstallDir" Value="[INSTALLDIR_USER]" Type="string" KeyPath="yes" />
						</RegistryKey>
						<Environment Id="UpdatePath_USER" Name="PATH" Action="set" Permanent="no" System="no" Part="last" Value="[INSTALLDIR]"/>
						<RemoveFolder Directory="INSTALLDIR_USER" On="uninstall"/>
						<RemoveFolder Directory="LocalAppProgramsFolder" On="uninstall"/>
					</Component>
				</Directory>
			</Directory>
		</StandardDirectory>

		<Component Id="MainExecutable_USER" Guid="*" Bitness="always64" Condition="MSIINSTALLPERUSER=1" Directory="INSTALLDIR">
			<File Id="MainExecutableFile_USER" Name="podman.exe" Source="artifacts/podman.exe" KeyPath="yes" />
		</Component>
		<Component Id="WinSshProxyExecutable_USER" Guid="*" Bitness="always64" Condition="MSIINSTALLPERUSER=1" Directory="INSTALLDIR">
			<File Id="WinSshProxyExecutableFile_USER" Name="win-sshproxy.exe" Source="artifacts/win-sshproxy.exe" KeyPath="yes" />
		</Component>
		<?if $(var.UseGVProxy) != Skip?>
		<Component Id="GvProxyExecutable_USER" Guid="*" Bitness="always64" Condition="MSIINSTALLPERUSER=1" Directory="INSTALLDIR">
			<File Id="GvProxyExecutableFile_USER" Name="gvproxy.exe" Source="artifacts/gvproxy.exe" KeyPath="yes" />
		</Component>
		<?endif?>
		<Component Id="GuideHTMLComponent_USER" Guid="*" Bitness="always64" Condition="MSIINSTALLPERUSER=1" Directory="INSTALLDIR">
			<File Id="GuideHTMLFile_USER" Name="welcome-podman.html" Source="docs/podman-for-windows.html" KeyPath="yes" />
		</Component>

		<Component Id="MachineProviderConfigFileUser" Guid="e1f7f469-a46c-446f-b10a-9fb3d1570732" Condition="CREATE_MACHINE_PROVIDER_CONFIG_FILE AND MSIINSTALLPERUSER=1" Transitive="true" Directory="CONFDIR">
			<CreateFolder />
			<IniFile Id="MachineProviderConfigFileUser" Action="createLine" Directory="CONFDIR" Section="machine" Name="99-podman-machine-provider.conf" Key="provider" Value="&quot;[MACHINE_PROVIDER]&quot;"/>
		</Component>

	</Package>
</Wix>
