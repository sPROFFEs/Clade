# Scaffold a fresh workpath directory with the standard layout.
# Usage:  new-workpath.ps1 -Name <name>
# Creates .\<name>\ in the current directory with mission.md +
# workpath.json + an HTML-comment-only personality.md. Optional
# tools/, agents/, knowledge/ are left out (create them when you
# have something to put in them).

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Name
)

# Schema requires ^[a-z0-9][a-z0-9_-]*$ for the workpath name.
if ($Name -notmatch '^[a-z0-9][a-z0-9_-]*$') {
    Write-Error "invalid workpath name: '$Name' (must match ^[a-z0-9][a-z0-9_-]*\$)"
    exit 1
}

if (Test-Path $Name) {
    Write-Error "already exists: $Name"
    exit 1
}

New-Item -ItemType Directory -Path $Name | Out-Null

$workpathJson = @"
{
  "description": "ONE-LINE summary -- edit me before first launch.",
  "version": "1"
}
"@
Set-Content -Path (Join-Path $Name "workpath.json") -Value $workpathJson -Encoding UTF8

$mission = @"
# $Name

> ONE-LINE description -- copy this to workpath.json:description too.

What this workpath is for, in 2-4 sentences. Be concrete about
what the agent does and how it behaves at a high level; the
playbook fills in the procedure.
"@
Set-Content -Path (Join-Path $Name "mission.md") -Value $mission -Encoding UTF8

$personality = @"
<!--
Persona file. Anything below this comment becomes the persona
prepended at the top of the compiled instructions. Comments-only
files are treated as "no persona" and produce no output.
-->
"@
Set-Content -Path (Join-Path $Name "personality.md") -Value $personality -Encoding UTF8

Write-Host "v scaffolded $Name/"
Write-Host "  next: edit $Name/mission.md and $Name/workpath.json,"
Write-Host "  then add playbook.md / rules.md / tools/ / agents/ /"
Write-Host "  knowledge/ as needed."
