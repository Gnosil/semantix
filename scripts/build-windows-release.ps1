param(
    [string]$Version = "0.1.0"
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$desktop = Join-Path $repo "cmd\semantix-desktop"
$release = Join-Path $repo "dist\semantix-desktop-$Version-windows-x64"
$wailsConfig = Get-Content -Raw -LiteralPath (Join-Path $desktop "wails.json") | ConvertFrom-Json
if ($wailsConfig.info.productVersion -ne $Version) {
    throw "Version $Version does not match wails.json productVersion $($wailsConfig.info.productVersion)"
}

New-Item -ItemType Directory -Force -Path $release | Out-Null
Push-Location $desktop
try {
    wails build -clean -platform windows/amd64 -webview2 embed -nsis -ldflags "-X main.desktopVersion=$Version"
} finally {
    Pop-Location
}

$exe = Join-Path $desktop "build\bin\Semantix.exe"
$installer = Get-ChildItem -LiteralPath (Join-Path $desktop "build\bin") -Filter "*-installer.exe" | Select-Object -First 1
if (-not (Test-Path -LiteralPath $exe)) { throw "Wails did not produce Semantix.exe" }
if (-not $installer) { throw "Wails did not produce the NSIS installer" }

$zip = Join-Path $release "Semantix-$Version-windows-x64-portable.zip"
Compress-Archive -LiteralPath $exe -DestinationPath $zip -Force
Copy-Item -LiteralPath $installer.FullName -Destination (Join-Path $release "Semantix-$Version-windows-x64-setup.exe") -Force

$checksums = Get-ChildItem -LiteralPath $release -File | ForEach-Object {
    $hash = Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName
    "$($hash.Hash.ToLowerInvariant())  $($_.Name)"
}
Set-Content -LiteralPath (Join-Path $release "SHA256SUMS.txt") -Value $checksums -Encoding ascii

Write-Host "Release artifacts: $release"
Write-Warning "The installer is unsigned. The GitHub Release must clearly explain the Windows SmartScreen prompt."
