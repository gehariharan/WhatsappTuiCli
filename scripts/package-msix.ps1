# Store packages remain unsigned; the Microsoft Store signs them after certification.
param(
 [Parameter(Mandatory=$true)][string]$Version,
 [Parameter(Mandatory=$true)][string]$PackageVersion,
 [Parameter(Mandatory=$true)][string]$IdentityName,
 [Parameter(Mandatory=$true)][string]$Publisher,
 [Parameter(Mandatory=$true)][string]$PublisherDisplayName,
 [switch]$TestIdentity
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
Set-Location (Join-Path $PSScriptRoot '..')
if ($Version -notmatch '^\d+\.\d+\.\d+(-[A-Za-z0-9][A-Za-z0-9.-]*)?\z') { throw 'Invalid application version' }
if ($PackageVersion -notmatch '^([1-9]\d*)\.(\d+)\.(\d+)\.0\z') { throw 'Store version must be A.B.C.0, with A greater than zero' }
foreach ($part in $PackageVersion.Split('.')) { if ([long]$part -gt 65535) { throw 'Store version components must be at most 65535' } }
if ($IdentityName -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]{1,48}[A-Za-z0-9]\z' -or $IdentityName -eq 'Store.Identity.Required') { throw 'Supply Package/Identity/Name from Partner Center' }
if ($Publisher -notmatch '^CN=\S' -or $Publisher -match '[\r\n]' -or $Publisher -eq 'CN=StoreIdentityRequired') { throw 'Supply Package/Identity/Publisher from Partner Center' }
if ([string]::IsNullOrWhiteSpace($PublisherDisplayName)) { throw 'Supply PublisherDisplayName from Partner Center' }
if (($IdentityName,$Publisher,$PublisherDisplayName) -match 'COPY_FROM|Store\.Identity|StoreIdentityRequired') { throw 'Replace placeholder metadata with the exact Partner Center product identity' }
if ($TestIdentity) {
 if ($IdentityName -ne 'Wasync.StoreIntegration' -or $Publisher -ne 'CN=WasyncMSIXTest') { throw 'Use the isolated CI identity with -TestIdentity' }
} elseif ($IdentityName -eq 'Wasync.StoreIntegration' -or $Publisher -eq 'CN=WasyncMSIXTest') { throw 'Test identity cannot be used for a Store submission' }
if ((go env GOHOSTOS) -ne 'windows' -or (go env GOHOSTARCH) -ne 'amd64') { throw 'Use a native Windows x64 builder' }
function Check-Exit { if ($LASTEXITCODE -ne 0) { throw "Command failed with exit code $LASTEXITCODE" } }
$sdk = Get-ChildItem "${env:ProgramFiles(x86)}/Windows Kits/10/bin" -Directory |
 Where-Object { $_.Name -match '^10\.0\.\d+\.0\z' -and (Test-Path (Join-Path $_.FullName 'x64/MakeAppx.exe')) } |
 Sort-Object { [version]$_.Name } -Descending | Select-Object -First 1
if (-not $sdk) { throw 'Install the Windows 10/11 SDK (MakeAppx.exe)' }
$out = Join-Path (Get-Location) "dist/store/$PackageVersion"
$temporaryRoot = $env:RUNNER_TEMP
if (-not $temporaryRoot) { $temporaryRoot = [IO.Path]::GetTempPath() }
$stage = Join-Path $temporaryRoot "wasync-msix-$([guid]::NewGuid())"
New-Item -ItemType Directory -Force $out, "$stage/Assets" | Out-Null
try {
 $commit = (git rev-parse HEAD).Trim(); Check-Exit
 $built = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
 $env:CGO_ENABLED = '1'
 $env:CGO_LDFLAGS = '-static'
 go build -trimpath -tags sqlite_fts5 -ldflags "-s -w -X github.com/gehariharan/wasync/internal/buildinfo.Version=$Version -X github.com/gehariharan/wasync/internal/buildinfo.Commit=$commit -X github.com/gehariharan/wasync/internal/buildinfo.BuiltAt=$built -X github.com/gehariharan/wasync/internal/buildinfo.Distribution=msix" -o "$stage/wasync.exe" ./cmd/wasync
 Check-Exit
 & "$stage/wasync.exe" version; Check-Exit
 Copy-Item README.md, LICENSE $stage
 python scripts/collect-licenses.py "$stage/THIRD_PARTY_LICENSES"; Check-Exit
 # Render our terminal mark in the existing wasync colors using Windows APIs.
 Add-Type -AssemblyName System.Drawing
 foreach ($asset in @(@('StoreLogo',50),@('Square44x44Logo',44),@('Square150x150Logo',150))) {
  $size = [int]$asset[1]
  $bitmap = [Drawing.Bitmap]::new($size,$size)
  $graphics = [Drawing.Graphics]::FromImage($bitmap)
  $pen = [Drawing.Pen]::new([Drawing.ColorTranslator]::FromHtml('#68D59B'),[single]($size*0.075))
  try {
   $graphics.Clear([Drawing.ColorTranslator]::FromHtml('#161C20'))
   $graphics.SmoothingMode = [Drawing.Drawing2D.SmoothingMode]::AntiAlias
   $graphics.DrawLine($pen,[single]($size*.22),[single]($size*.30),[single]($size*.43),[single]($size*.50))
   $graphics.DrawLine($pen,[single]($size*.43),[single]($size*.50),[single]($size*.22),[single]($size*.70))
   $graphics.DrawLine($pen,[single]($size*.51),[single]($size*.70),[single]($size*.78),[single]($size*.70))
   $bitmap.Save("$stage/Assets/$($asset[0]).png",[Drawing.Imaging.ImageFormat]::Png)
  } finally { $pen.Dispose(); $graphics.Dispose(); $bitmap.Dispose() }
 }
 [xml]$manifest = Get-Content packaging/store/AppxManifest.xml -Raw
 $manifest.Package.Identity.SetAttribute('Name',$IdentityName)
 $manifest.Package.Identity.SetAttribute('Publisher',$Publisher)
 $manifest.Package.Identity.SetAttribute('Version',$PackageVersion)
 $manifest.Package.Properties.PublisherDisplayName = $PublisherDisplayName
 if ($TestIdentity) {
  $namespaces = [Xml.XmlNamespaceManager]::new($manifest.NameTable)
  $namespaces.AddNamespace('desktop','http://schemas.microsoft.com/appx/manifest/desktop/windows10')
  $manifest.SelectSingleNode('//desktop:ExecutionAlias',$namespaces).SetAttribute('Alias','wasync-msix-test.exe')
 }
 $settings = [Xml.XmlWriterSettings]::new()
 $settings.Encoding = [Text.UTF8Encoding]::new($false)
 $settings.Indent = $true
 $writer = [Xml.XmlWriter]::Create("$stage/AppxManifest.xml",$settings)
 try { $manifest.Save($writer) } finally { $writer.Dispose() }
 $name = "wasync_${PackageVersion}_x64"
 $package = "$out/$name.msix"
 & "$($sdk.FullName)/x64/MakeAppx.exe" pack /d $stage /p $package /o
 Check-Exit
 # Store upload container: MSIX only, no PFX, local test cert, or user data.
 if (-not $TestIdentity) {
  Compress-Archive -Path $package -DestinationPath "$out/$name.zip" -Force
  Move-Item "$out/$name.zip" "$out/$name.msixupload" -Force
 }
 @{ packageVersion=$PackageVersion; applicationVersion=$Version; identityName=$IdentityName; publisher=$Publisher; publisherDisplayName=$PublisherDisplayName; commit=$commit; architecture='x64'; storeSubmission=(-not $TestIdentity); signed=$false; dataDirectory='%USERPROFILE%\.wasync'; alias=$(if($TestIdentity){'wasync-msix-test.exe'}else{'wasync.exe'}) } |
 ConvertTo-Json | Set-Content -Encoding utf8 "$out/submission-info.json"
 Get-ChildItem $out -File | Where-Object Extension -in '.msix','.msixupload' | ForEach-Object {
  "$((Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower())  $($_.Name)" | Set-Content -Encoding ascii "$($_.FullName).sha256"
 }
 Write-Host "Unsigned Store packages: $out"
} finally { Remove-Item -Recurse -Force $stage }
