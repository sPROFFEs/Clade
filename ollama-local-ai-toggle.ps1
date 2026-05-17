param(
    [switch]$SessionOnly
)

$ErrorActionPreference = "Stop"

$ProviderName = "ollama_remote"
$ProfileName = "ollama_remote"
$OpenCodeProviderName = "ollama_remote"
$CodexConfigDir = Join-Path $HOME ".codex"
$CodexConfig = Join-Path $CodexConfigDir "config.toml"
$OpenCodeConfigDir = if ($env:XDG_CONFIG_HOME) { Join-Path $env:XDG_CONFIG_HOME "opencode" } else { Join-Path $HOME ".config\opencode" }
$OpenCodeConfig = Join-Path $OpenCodeConfigDir "opencode.json"

function Write-Title($Text) {
    Write-Host ""
    Write-Host "== $Text ==" -ForegroundColor Cyan
}

function Read-Default($Prompt, $Default) {
    $value = Read-Host "$Prompt [$Default]"
    if ([string]::IsNullOrWhiteSpace($value)) { return $Default }
    return $value.Trim()
}

# Accept both y/Y (English) and s/S (legacy Spanish) so the script
# stays backwards-compatible with people who scripted it pre-rename.
function Is-Yes($Value) {
    $v = "$Value".Trim().ToLower()
    return $v.StartsWith("s") -or $v.StartsWith("y")
}

function Normalize-Endpoint($Endpoint) {
    $endpoint = $Endpoint.Trim().TrimEnd("/")
    if ($endpoint -notmatch "^https?://") {
        $endpoint = "http://$endpoint"
    }
    return $endpoint.TrimEnd("/")
}

function Get-OllamaModels($Endpoint) {
    $models = @()

    try {
        $tags = Invoke-RestMethod -Uri "$Endpoint/api/tags" -Method Get -TimeoutSec 8
        if ($tags.models) {
            $models += $tags.models | ForEach-Object { $_.name }
        }
    } catch {}

    if ($models.Count -eq 0) {
        try {
            $openAi = Invoke-RestMethod -Uri "$Endpoint/v1/models" -Method Get -TimeoutSec 8
            if ($openAi.data) {
                $models += $openAi.data | ForEach-Object { $_.id }
            }
        } catch {}
    }

    return @($models | Where-Object { $_ } | Sort-Object -Unique)
}

function Select-Model($Models) {
    if ($Models.Count -eq 0) {
        return Read-Default "Couldn't detect any models. Type the exact name" "qwen3-coder"
    }

    Write-Host ""
    Write-Host "Detected models:"
    for ($i = 0; $i -lt $Models.Count; $i++) {
        Write-Host ("  {0}. {1}" -f ($i + 1), $Models[$i])
    }

    while ($true) {
        $choice = Read-Host "Pick a number or type a model name"
        if ($choice -match "^\d+$") {
            $idx = [int]$choice - 1
            if ($idx -ge 0 -and $idx -lt $Models.Count) { return $Models[$idx] }
        }
        if (-not [string]::IsNullOrWhiteSpace($choice)) { return $choice.Trim() }
    }
}

function Set-UserEnv($Name, $Value) {
    if ($SessionOnly) {
        Set-Item -Path "Env:$Name" -Value $Value
    } else {
        [Environment]::SetEnvironmentVariable($Name, $Value, "User")
        Set-Item -Path "Env:$Name" -Value $Value
    }
}

function Remove-UserEnv($Name) {
    if ($SessionOnly) {
        Remove-Item -Path "Env:$Name" -ErrorAction SilentlyContinue
    } else {
        [Environment]::SetEnvironmentVariable($Name, $null, "User")
        Remove-Item -Path "Env:$Name" -ErrorAction SilentlyContinue
    }
}

function Remove-TomlTable($Content, $HeaderRegex) {
    $lines = $Content -split "`r?`n"
    $out = New-Object System.Collections.Generic.List[string]
    $skip = $false

    foreach ($line in $lines) {
        if ($line -match $HeaderRegex) {
            $skip = $true
            continue
        }

        if ($skip -and $line -match "^\s*\[") {
            $skip = $false
        }

        if (-not $skip) {
            $out.Add($line)
        }
    }

    return ($out -join "`n").TrimEnd()
}

function Update-CodexConfig($Endpoint, $Model, $MakeDefault, $WireApi) {
    New-Item -ItemType Directory -Force -Path $CodexConfigDir | Out-Null
    if (-not (Test-Path $CodexConfig)) {
        New-Item -ItemType File -Path $CodexConfig | Out-Null
    }

    $backup = "$CodexConfig.bak-$(Get-Date -Format yyyyMMdd-HHmmss)"
    Copy-Item $CodexConfig $backup

    $content = Get-Content $CodexConfig -Raw
    $content = Remove-TomlTable $content "^\s*\[model_providers\.$ProviderName\]\s*$"
    $content = Remove-TomlTable $content "^\s*\[profiles\.$ProfileName\]\s*$"

    if ($MakeDefault) {
        $content = ($content -split "`r?`n" | Where-Object {
            $_ -notmatch '^\s*model_provider\s*=' -and $_ -notmatch '^\s*model\s*='
        }) -join "`n"
        $content = "model_provider = `"$ProviderName`"`nmodel = `"$Model`"`n" + $content.TrimStart()
    }

    $baseUrl = "$Endpoint/v1"
    $block = @"

[model_providers.$ProviderName]
name = "Ollama Remote"
base_url = "$baseUrl"
env_key = "OPENAI_API_KEY"
wire_api = "$WireApi"

[profiles.$ProfileName]
model_provider = "$ProviderName"
model = "$Model"
"@

    $newContent = ($content.TrimEnd() + "`n" + $block.TrimEnd() + "`n")
    Set-Content -Path $CodexConfig -Value $newContent -Encoding UTF8
    return $backup
}

function Disable-CodexConfig() {
    if (-not (Test-Path $CodexConfig)) { return $null }
    $backup = "$CodexConfig.bak-$(Get-Date -Format yyyyMMdd-HHmmss)"
    Copy-Item $CodexConfig $backup
    $content = Get-Content $CodexConfig -Raw
    $content = Remove-TomlTable $content "^\s*\[model_providers\.$ProviderName\]\s*$"
    $content = Remove-TomlTable $content "^\s*\[profiles\.$ProfileName\]\s*$"
    $content = ($content -split "`r?`n" | Where-Object {
        $_ -notmatch "^\s*model_provider\s*=\s*`"$ProviderName`"\s*$"
    }) -join "`n"
    Set-Content -Path $CodexConfig -Value ($content.TrimEnd() + "`n") -Encoding UTF8
    return $backup
}

function ConvertFrom-JsonHashtable($Text) {
    try {
        return $Text | ConvertFrom-Json -AsHashtable
    } catch {
        $obj = $Text | ConvertFrom-Json
        return ConvertTo-Hashtable $obj
    }
}

function ConvertTo-Hashtable($InputObject) {
    if ($null -eq $InputObject) { return $null }
    if ($InputObject -is [System.Collections.IDictionary]) { return $InputObject }
    if ($InputObject -is [System.Collections.IEnumerable] -and $InputObject -isnot [string]) {
        return @($InputObject | ForEach-Object { ConvertTo-Hashtable $_ })
    }
    if ($InputObject.PSObject.Properties.Count -eq 0) { return $InputObject }
    $hash = @{}
    foreach ($prop in $InputObject.PSObject.Properties) {
        $hash[$prop.Name] = ConvertTo-Hashtable $prop.Value
    }
    return $hash
}

function Update-OpenCodeConfig($Endpoint, $Model, $MakeDefault) {
    New-Item -ItemType Directory -Force -Path $OpenCodeConfigDir | Out-Null
    if (Test-Path $OpenCodeConfig) {
        $backup = "$OpenCodeConfig.bak-$(Get-Date -Format yyyyMMdd-HHmmss)"
        Copy-Item $OpenCodeConfig $backup
        try {
            $json = ConvertFrom-JsonHashtable (Get-Content $OpenCodeConfig -Raw)
        } catch {
            throw "Couldn't parse $OpenCodeConfig as JSON. Backup created at $backup"
        }
    } else {
        $backup = $null
        $json = @{}
    }

    $json["`$schema"] = "https://opencode.ai/config.json"
    if (-not $json.ContainsKey("provider") -or $null -eq $json["provider"]) {
        $json["provider"] = @{}
    }

    $json["provider"][$OpenCodeProviderName] = @{
        npm = "@ai-sdk/openai-compatible"
        name = "Ollama Remote"
        options = @{
            baseURL = "$Endpoint/v1"
        }
        models = @{
            $Model = @{
                name = $Model
            }
        }
    }

    if ($MakeDefault) {
        $json["model"] = "$OpenCodeProviderName/$Model"
        $json["small_model"] = "$OpenCodeProviderName/$Model"
    }

    $json | ConvertTo-Json -Depth 20 | Set-Content -Path $OpenCodeConfig -Encoding UTF8
    return $backup
}

function Disable-OpenCodeConfig() {
    if (-not (Test-Path $OpenCodeConfig)) { return $null }
    $backup = "$OpenCodeConfig.bak-$(Get-Date -Format yyyyMMdd-HHmmss)"
    Copy-Item $OpenCodeConfig $backup
    $json = ConvertFrom-JsonHashtable (Get-Content $OpenCodeConfig -Raw)

    if ($json.ContainsKey("provider") -and $json["provider"].ContainsKey($OpenCodeProviderName)) {
        $json["provider"].Remove($OpenCodeProviderName)
    }
    foreach ($key in @("model", "small_model")) {
        if ($json.ContainsKey($key) -and "$($json[$key])".StartsWith("$OpenCodeProviderName/")) {
            $json.Remove($key)
        }
    }

    $json | ConvertTo-Json -Depth 20 | Set-Content -Path $OpenCodeConfig -Encoding UTF8
    return $backup
}

function Enable-LocalModels() {
    Write-Title "Ollama endpoint"
    $endpoint = Normalize-Endpoint (Read-Default "Remote endpoint" "http://192.168.1.50:11434")

    Write-Host "Probing $endpoint ..."
    $models = Get-OllamaModels $endpoint
    if ($models.Count -eq 0) {
        Write-Host "No models detected. Check the firewall, OLLAMA_HOST, and /api/tags." -ForegroundColor Yellow
    }

    $model = Select-Model $models

    Write-Title "Tools"
    $enableClaude = Is-Yes (Read-Default "Configure Claude Code? (y/n)" "y")
    $enableCodex = Is-Yes (Read-Default "Configure Codex CLI? (y/n)" "y")
    $enableOpenCode = Is-Yes (Read-Default "Configure OpenCode? (y/n)" "y")

    if ($enableClaude) {
        Set-UserEnv "ANTHROPIC_AUTH_TOKEN" "ollama"
        Set-UserEnv "ANTHROPIC_API_KEY" ""
        Set-UserEnv "ANTHROPIC_BASE_URL" $endpoint
        Write-Host "Claude Code configured. Use: claude --model $model"
    }

    if ($enableCodex) {
        Set-UserEnv "OPENAI_API_KEY" "ollama"
        $makeDefault = Is-Yes (Read-Default "Make Ollama the default for Codex? If no, run codex -p $ProfileName (y/n)" "n")
        $wireApi = Read-Default "Wire API for Codex: chat or responses" "chat"
        if ($wireApi -notin @("chat", "responses")) { $wireApi = "chat" }
        $backup = Update-CodexConfig $endpoint $model $makeDefault $wireApi
        Write-Host "Codex configured at $CodexConfig"
        Write-Host "Backup: $backup"
        Write-Host "Recommended usage: codex -p $ProfileName"
    }

    if ($enableOpenCode) {
        $makeDefault = Is-Yes (Read-Default "Make Ollama the default for OpenCode? (y/n)" "y")
        $backup = Update-OpenCodeConfig $endpoint $model $makeDefault
        Write-Host "OpenCode configured at $OpenCodeConfig"
        if ($backup) { Write-Host "Backup: $backup" }
        Write-Host "Use: opencode, then /models if you want to change the model"
    }
}

function Disable-LocalModels() {
    Write-Title "Disable"
    $disableClaude = Is-Yes (Read-Default "Remove Claude Code env vars? (y/n)" "y")
    $disableCodex = Is-Yes (Read-Default "Remove Ollama provider/profile from Codex? (y/n)" "y")
    $disableOpenCode = Is-Yes (Read-Default "Remove Ollama provider from OpenCode? (y/n)" "y")

    if ($disableClaude) {
        Remove-UserEnv "ANTHROPIC_AUTH_TOKEN"
        Remove-UserEnv "ANTHROPIC_API_KEY"
        Remove-UserEnv "ANTHROPIC_BASE_URL"
        Write-Host "Claude Code env vars removed."
    }

    if ($disableCodex) {
        $backup = Disable-CodexConfig
        if ($backup) {
            Write-Host "Codex blocks removed. Backup: $backup"
        } else {
            Write-Host "No Codex config found."
        }
    }

    if ($disableOpenCode) {
        $backup = Disable-OpenCodeConfig
        if ($backup) {
            Write-Host "OpenCode provider removed. Backup: $backup"
        } else {
            Write-Host "No OpenCode config found."
        }
    }
}

function Show-Status() {
    Write-Title "Status"
    Write-Host "ANTHROPIC_BASE_URL = $env:ANTHROPIC_BASE_URL"
    Write-Host "ANTHROPIC_AUTH_TOKEN = $env:ANTHROPIC_AUTH_TOKEN"
    Write-Host "OPENAI_API_KEY set = $([bool]$env:OPENAI_API_KEY)"
    Write-Host "Codex config = $CodexConfig"
    if (Test-Path $CodexConfig) {
        Select-String -Path $CodexConfig -Pattern "ollama_remote|model_provider|base_url|wire_api|^\s*model\s*=" | ForEach-Object {
            Write-Host $_.Line
        }
    }
    Write-Host "OpenCode config = $OpenCodeConfig"
    if (Test-Path $OpenCodeConfig) {
        Select-String -Path $OpenCodeConfig -Pattern "ollama_remote|baseURL|model|small_model" | ForEach-Object {
            Write-Host $_.Line
        }
    }
}

while ($true) {
    Write-Title "Remote Ollama for Claude Code / Codex CLI / OpenCode"
    Write-Host "1. Enable / configure"
    Write-Host "2. Disable"
    Write-Host "3. Status"
    Write-Host "4. Quit"
    $choice = Read-Host "Option"

    switch ($choice) {
        "1" { Enable-LocalModels }
        "2" { Disable-LocalModels }
        "3" { Show-Status }
        "4" { break }
        default { Write-Host "Invalid option" -ForegroundColor Yellow }
    }
}
