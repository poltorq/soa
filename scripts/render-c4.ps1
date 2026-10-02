param(
    [ValidateSet("svg", "png")]
    [string]$Format = "svg"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$rootDir = Split-Path -Parent $PSScriptRoot
$sourceFile = Join-Path $rootDir "diagrams/c4-container.puml"
$outputDir = Join-Path $rootDir "diagrams/out"
$outputFile = Join-Path $outputDir "c4-container.$Format"
$plantUml = Get-Command plantuml -ErrorAction SilentlyContinue

if ($null -eq $plantUml) {
    throw 'PlantUML was not found. Run "make install" first.'
}

New-Item -ItemType Directory -Force -Path $outputDir | Out-Null

& $plantUml.Source "-t$Format" "-charset" "UTF-8" "-failfast2" "-o" $outputDir $sourceFile
if ($LASTEXITCODE -ne 0) {
    throw "PlantUML failed with exit code $LASTEXITCODE."
}

if (-not (Test-Path $outputFile -PathType Leaf)) {
    throw "PlantUML finished, but $outputFile was not created."
}

Write-Host "C4 diagram: $outputFile"
