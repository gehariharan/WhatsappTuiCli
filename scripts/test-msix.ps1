# CI-only install/upgrade/uninstall test. Never uses a real WhatsApp account.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted') { throw 'Run this fixture only on a disposable GitHub-hosted Windows runner' }
Set-Location (Join-Path $PSScriptRoot '..')
$identity = 'Wasync.StoreIntegration'
$data = Join-Path $env:USERPROFILE '.wasync'
if ((Test-Path $data) -or (Get-AppxPackage -Name $identity)) { throw 'Refusing to touch an existing archive or test package' }
function Check-Exit { if ($LASTEXITCODE -ne 0) { throw "Command failed with exit code $LASTEXITCODE" } }
$sdk = Get-ChildItem "${env:ProgramFiles(x86)}/Windows Kits/10/bin" -Directory |
 Where-Object { $_.Name -match '^10\.0\.\d+\.0$' -and (Test-Path (Join-Path $_.FullName 'x64/SignTool.exe')) } |
 Sort-Object { [version]$_.Name } -Descending | Select-Object -First 1
if (-not $sdk) { throw 'Windows SDK signing tool missing' }
$temp = Join-Path $env:RUNNER_TEMP "wasync-msix-test-$([guid]::NewGuid())"
New-Item -ItemType Directory $temp | Out-Null
$cert = $null
$trusted = $null
$oldData = $env:WASYNC_DATA_DIR
$policyPath = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock'
$oldPolicy = $null
$policyKey = Get-ItemProperty -Path $policyPath -ErrorAction SilentlyContinue
if ($policyKey -and $policyKey.PSObject.Properties['AllowAllTrustedApps']) { $oldPolicy = $policyKey.AllowAllTrustedApps }
try {
 # Only the CI machine trusts this short-lived, nonexportable test certificate.
 $cert = New-SelfSignedCertificate -Type CodeSigningCert -Subject 'CN=WasyncMSIXTest' -CertStoreLocation Cert:\CurrentUser\My -KeyExportPolicy NonExportable -NotAfter (Get-Date).AddDays(2)
 Export-Certificate -Cert $cert -FilePath "$temp/test.cer" | Out-Null
 $trusted = Import-Certificate -FilePath "$temp/test.cer" -CertStoreLocation Cert:\LocalMachine\TrustedPeople
 New-Item -Path $policyPath -Force | Out-Null
 Set-ItemProperty -Path $policyPath -Name AllowAllTrustedApps -Value 1 -Type DWord
 Remove-Item Env:WASYNC_DATA_DIR -ErrorAction SilentlyContinue
 $alias = Join-Path $env:LOCALAPPDATA 'Microsoft/WindowsApps/wasync-msix-test.exe'
 foreach ($revision in 1,2) {
  $packageVersion = "1.0.$revision.0"
  ./scripts/package-msix.ps1 -Version "0.1.0-ci.$revision" -PackageVersion $packageVersion -IdentityName $identity -Publisher 'CN=WasyncMSIXTest' -PublisherDisplayName 'wasync CI test' -TestIdentity
  # Test copies are signed locally. The build/upload outputs remain unsigned.
  $source = "dist/store/$packageVersion/wasync_${packageVersion}_x64.msix"
  $testPackage = "$temp/test-$revision.msix"
  Copy-Item $source $testPackage
  & "$($sdk.FullName)/x64/SignTool.exe" sign /fd SHA256 /sha1 $cert.Thumbprint /s My $testPackage
  Check-Exit
  Add-AppxPackage -Path $testPackage
  $installed = Get-AppxPackage -Name $identity
  if ($installed.Version -ne $packageVersion) { throw 'MSIX version was not installed' }
  $version = & $alias version; Check-Exit
  if ($version -notmatch "0\.1\.0-ci\.$revision") { throw 'Execution alias did not activate the expected build' }
  if ($revision -eq 1) {
   & $alias status; Check-Exit
   if (-not (Test-Path "$data/archive.db")) { throw 'Store build did not create its external default archive' }
   $seed = @'
import sqlite3,sys,pathlib
root=pathlib.Path(sys.argv[1])
with sqlite3.connect(root/'archive.db') as db:
 db.execute("INSERT INTO accounts(id,jid,created_at) VALUES(1,'synthetic@lid',1700000000000)")
 db.execute("INSERT INTO app_state(id,account_id) VALUES(1,1)")
 db.execute("INSERT INTO groups(account_id,group_id,name,sync_enabled,created_at,updated_at) VALUES(1,'100@g.us','Synthetic Store group',1,1700000000000,1700000000000)")
 db.execute("INSERT INTO messages(account_id,group_id,message_id,sender_id,timestamp,message_type,text_content,source,has_original,received_at) VALUES(1,'100@g.us','fixture','200@lid',1700000000000,'text','MSIX upgrade fixture','live',1,1700000000000)")
with sqlite3.connect(root/'session.db') as session:
 session.execute("CREATE TABLE ci_session_marker(value TEXT)")
 session.execute("INSERT INTO ci_session_marker VALUES('CI-only synthetic session')")
(root/'session-test-marker').write_bytes(b'CI-only synthetic credential marker')
(root/'media').mkdir()
(root/'media'/'test-marker').write_bytes(b'CI-only synthetic media marker')
'@
   python -c $seed $data; Check-Exit
  }
  $result = (& $alias query search 'MSIX upgrade fixture' --chat '100@g.us' | Out-String | ConvertFrom-Json); Check-Exit
  if ($result.items.Count -ne 1 -or $result.items[0].text_content -ne 'MSIX upgrade fixture') { throw 'Archive query failed after installation/upgrade' }
  if ((Get-Content "$data/session-test-marker" -Raw) -ne 'CI-only synthetic credential marker') { throw 'Session marker changed during upgrade' }
  if ((Get-Content "$data/media/test-marker" -Raw) -ne 'CI-only synthetic media marker') { throw 'Media marker changed during upgrade' }
 }
 Remove-AppxPackage -Package (Get-AppxPackage -Name $identity).PackageFullName
 foreach ($file in 'archive.db','session.db','session-test-marker','media/test-marker') {
  if (-not (Test-Path (Join-Path $data $file))) { throw "Uninstall removed retained data: $file" }
 }
 $verify = @'
import sqlite3,sys,pathlib
root=pathlib.Path(sys.argv[1])
with sqlite3.connect(root/'archive.db') as db:
 assert db.execute("SELECT text_content FROM messages WHERE message_id='fixture'").fetchone()[0]=='MSIX upgrade fixture'
'@
 python -c $verify $data; Check-Exit
 Write-Host 'MSIX execution alias, FTS query, upgrade and retained archive/session/media passed.'
} finally {
 $installed = Get-AppxPackage -Name $identity
 if ($installed) { Remove-AppxPackage -Package $installed.PackageFullName }
 if ($trusted) { Remove-Item "Cert:/LocalMachine/TrustedPeople/$($trusted.Thumbprint)" -ErrorAction SilentlyContinue }
 if ($cert) { Remove-Item "Cert:/CurrentUser/My/$($cert.Thumbprint)" -DeleteKey -ErrorAction SilentlyContinue }
 if ($null -ne $oldPolicy) { Set-ItemProperty -Path $policyPath -Name AllowAllTrustedApps -Value $oldPolicy -Type DWord }
 else { Remove-ItemProperty -Path $policyPath -Name AllowAllTrustedApps -ErrorAction SilentlyContinue }
 $env:WASYNC_DATA_DIR = $oldData
 Remove-Item -Recurse -Force $temp -ErrorAction SilentlyContinue
 # Existing-data guard above guarantees this directory belongs to this fixture.
 Remove-Item -Recurse -Force $data -ErrorAction SilentlyContinue
}
