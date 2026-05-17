# Count lines in a file
param([string]$Path)
(Get-Content $Path | Measure-Object -Line).Lines
