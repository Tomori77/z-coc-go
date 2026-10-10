<#
.SYNOPSIS
    Prepare item art PNGs for the /media/items mount.

.DESCRIPTION
    Converts PNGs from -SourceDir to WebP (RGBA preserved) using Python PIL when
    available; otherwise copies the PNG unchanged with a warning. Images are
    resized so the largest dimension is <= -Max. For every processed file the
    absolute output path and the URL to paste into the admin settings are printed.

.PARAMETER SourceDir
    Directory containing source PNGs. Default: D:\1orca\coc\docs\未处理图片

.PARAMETER OutDir
    Output directory. Default: <repo>\media\items

.PARAMETER Max
    Maximum dimension (width or height) in pixels. Default: 512

.PARAMETER Quality
    WebP quality (1-100). Default: 82

.PARAMETER Name
    Explicit output basename for a single file (no extension).

.PARAMETER All
    Convert every PNG in SourceDir instead of just the first one.

.EXAMPLE
    tools\prepare-item-art.ps1 -All

.EXAMPLE
    tools\prepare-item-art.ps1 -Name sing
#>
[CmdletBinding()]
param(
    [string]$SourceDir = 'D:\1orca\coc\docs\未处理图片',
    [string]$OutDir,
    [int]$Max = 512,
    [int]$Quality = 82,
    [string]$Name,
    [switch]$All
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
if (-not $OutDir) { $OutDir = Join-Path $repoRoot 'media\items' }
$OutDir = [System.IO.Path]::GetFullPath($OutDir)

if (-not (Test-Path -LiteralPath $SourceDir)) { throw "SourceDir not found: $SourceDir" }
if (-not (Test-Path -LiteralPath $OutDir)) { New-Item -ItemType Directory -Path $OutDir -Force | Out-Null }

$pngs = @(Get-ChildItem -LiteralPath $SourceDir -Filter *.png -File | Sort-Object Name)
if ($pngs.Count -eq 0) { throw "No PNG files found in $SourceDir" }
if ($All -and $Name) { Write-Warning '-Name is ignored when -All is specified.' }

$targets = if ($All) { $pngs } else { @($pngs[0]) }

$pyExe = $null
foreach ($candidate in @('python', 'py')) {
    try {
        & $candidate -c 'import PIL' 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) { $pyExe = $candidate; break }
    } catch { }
}

$pyCode = @'
import sys
from PIL import Image
src, out, mx, q = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
im = Image.open(src)
if im.mode != 'RGBA':
    im = im.convert('RGBA')
im.thumbnail((mx, mx), Image.LANCZOS)
im.save(out, 'WEBP', quality=q, method=6)
'@

$mediaRoot = Join-Path $repoRoot 'media'
$fallbackIndex = 0

foreach ($src in $targets) {
    if ($Name) {
        $base = $Name
    } else {
        $raw = [System.IO.Path]::GetFileNameWithoutExtension($src.Name)
        $slug = $raw.ToLowerInvariant() -replace '[^a-z0-9\-_]', ''
        if ([string]::IsNullOrEmpty($slug)) { $fallbackIndex++; $slug = "item$fallbackIndex" }
        $base = $slug
    }

    $converted = $false
    $outPath = Join-Path $OutDir "$base.webp"
    if ($pyExe) {
        & $pyExe -c $pyCode $src.FullName $outPath $Max $Quality
        if ($LASTEXITCODE -eq 0 -and (Test-Path -LiteralPath $outPath)) {
            $converted = $true
        } else {
            Write-Warning "Python conversion failed for $($src.Name); copying PNG instead."
        }
    } else {
        Write-Warning 'Python/PIL not available; copying PNG unchanged (no resize/conversion).'
    }

    if (-not $converted) {
        $outPath = Join-Path $OutDir "$base.png"
        Copy-Item -LiteralPath $src.FullName -Destination $outPath -Force
    }

    if ($outPath.StartsWith($mediaRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
        $rel = $outPath.Substring($mediaRoot.Length).TrimStart('\', '/') -replace '\\', '/'
        $url = "/media/$rel"
    } else {
        $url = "/media/$([System.IO.Path]::GetFileName($outPath))"
    }

    Write-Output "FILE: $outPath"
    Write-Output "URL:  $url"
    Write-Output ''
}
