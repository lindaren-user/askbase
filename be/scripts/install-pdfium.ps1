param(
    [string]$Version = "8057"
)

$ErrorActionPreference = "Stop"
$backendRoot = Split-Path -Parent $PSScriptRoot
$targetDirectory = Join-Path $backendRoot "lib"
$targetPath = Join-Path $targetDirectory "pdfium.dll"
$archiveUrl = "https://github.com/bblanchon/pdfium-binaries/releases/download/chromium/$Version/pdfium-win-x64.tgz"
$expectedDLLHash = "55E7EBEF29A1EC9523D1ADB8B260A73E7DFB0F64D3F0285121D20ECD6148EF18"
$temporaryDirectory = Join-Path ([System.IO.Path]::GetTempPath()) ("askbase-pdfium-" + [guid]::NewGuid())
$archivePath = Join-Path $temporaryDirectory "pdfium-win-x64.tgz"

New-Item -ItemType Directory -Force -Path $temporaryDirectory | Out-Null
New-Item -ItemType Directory -Force -Path $targetDirectory | Out-Null

try {
    Write-Host "Downloading PDFium chromium/$Version..."
    & curl.exe --fail --location --retry 5 --retry-all-errors --output $archivePath $archiveUrl
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to download PDFium (curl exit code: $LASTEXITCODE)"
    }
    $windowsTar = Join-Path $env:SystemRoot "System32\tar.exe"
    & $windowsTar -xzf $archivePath -C $temporaryDirectory
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to extract PDFium (tar exit code: $LASTEXITCODE)"
    }

    $extractedDLL = Join-Path $temporaryDirectory "bin\pdfium.dll"
    if (-not (Test-Path -LiteralPath $extractedDLL)) {
        throw "The PDFium archive does not contain bin\pdfium.dll"
    }

    if ($Version -eq "8057") {
        $actualDLLHash = (Get-FileHash -LiteralPath $extractedDLL -Algorithm SHA256).Hash
        if ($actualDLLHash -ne $expectedDLLHash) {
            throw "PDFium DLL checksum mismatch"
        }
    }

    Copy-Item -LiteralPath $extractedDLL -Destination $targetPath -Force
    Write-Host "Installed PDFium: $targetPath"
}
finally {
    if (Test-Path -LiteralPath $temporaryDirectory) {
        Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force
    }
}
