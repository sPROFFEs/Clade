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
        return Read-Default "No he podido detectar modelos. Escribe el nombre exacto" "qwen3-coder"
    }

    Write-Host ""
    Write-Host "Modelos detectados:"
    for ($i = 0; $i -lt $Models.Count; $i++) {
        Write-Host ("  {0}. {1}" -f ($i + 1), $Models[$i])
    }

    while ($true) {
        $choice = Read-Host "Elige numero o escribe un modelo"
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
            throw "No puedo parsear $OpenCodeConfig como JSON. Backup creado en $backup"
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
    Write-Title "Endpoint Ollama"
    $endpoint = Normalize-Endpoint (Read-Default "Endpoint remoto" "http://192.168.1.50:11434")

    Write-Host "Probando $endpoint ..."
    $models = Get-OllamaModels $endpoint
    if ($models.Count -eq 0) {
        Write-Host "No se han detectado modelos. Revisa firewall, OLLAMA_HOST y /api/tags." -ForegroundColor Yellow
    }

    $model = Select-Model $models

    Write-Title "Herramientas"
    $enableClaude = Is-Yes (Read-Default "Configurar Claude Code? (s/n)" "s")
    $enableCodex = Is-Yes (Read-Default "Configurar Codex CLI? (s/n)" "s")
    $enableOpenCode = Is-Yes (Read-Default "Configurar OpenCode? (s/n)" "s")

    if ($enableClaude) {
        Set-UserEnv "ANTHROPIC_AUTH_TOKEN" "ollama"
        Set-UserEnv "ANTHROPIC_API_KEY" ""
        Set-UserEnv "ANTHROPIC_BASE_URL" $endpoint
        Write-Host "Claude Code configurado. Uso: claude --model $model"
    }

    if ($enableCodex) {
        Set-UserEnv "OPENAI_API_KEY" "ollama"
        $makeDefault = Is-Yes (Read-Default "Hacer Ollama el default de Codex? Si dices no, usa codex -p $ProfileName (s/n)" "n")
        $wireApi = Read-Default "Wire API para Codex: chat o responses" "chat"
        if ($wireApi -notin @("chat", "responses")) { $wireApi = "chat" }
        $backup = Update-CodexConfig $endpoint $model $makeDefault $wireApi
        Write-Host "Codex configurado en $CodexConfig"
        Write-Host "Backup: $backup"
        Write-Host "Uso recomendado: codex -p $ProfileName"
    }

    if ($enableOpenCode) {
        $makeDefault = Is-Yes (Read-Default "Hacer Ollama el default de OpenCode? (s/n)" "s")
        $backup = Update-OpenCodeConfig $endpoint $model $makeDefault
        Write-Host "OpenCode configurado en $OpenCodeConfig"
        if ($backup) { Write-Host "Backup: $backup" }
        Write-Host "Uso: opencode y luego /models si quieres cambiar modelo"
    }
}

function Disable-LocalModels() {
    Write-Title "Deshabilitar"
    $disableClaude = Is-Yes (Read-Default "Quitar variables de Claude Code? (s/n)" "s")
    $disableCodex = Is-Yes (Read-Default "Quitar provider/profile Ollama de Codex? (s/n)" "s")
    $disableOpenCode = Is-Yes (Read-Default "Quitar provider Ollama de OpenCode? (s/n)" "s")

    if ($disableClaude) {
        Remove-UserEnv "ANTHROPIC_AUTH_TOKEN"
        Remove-UserEnv "ANTHROPIC_API_KEY"
        Remove-UserEnv "ANTHROPIC_BASE_URL"
        Write-Host "Variables de Claude Code eliminadas."
    }

    if ($disableCodex) {
        $backup = Disable-CodexConfig
        if ($backup) {
            Write-Host "Bloques de Codex eliminados. Backup: $backup"
        } else {
            Write-Host "No existe config de Codex."
        }
    }

    if ($disableOpenCode) {
        $backup = Disable-OpenCodeConfig
        if ($backup) {
            Write-Host "Provider de OpenCode eliminado. Backup: $backup"
        } else {
            Write-Host "No existe config de OpenCode."
        }
    }
}

function Show-Status() {
    Write-Title "Estado"
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
    Write-Title "Ollama remoto para Claude Code / Codex CLI"
    Write-Host "1. Habilitar/configurar"
    Write-Host "2. Deshabilitar"
    Write-Host "3. Estado"
    Write-Host "4. Salir"
    $choice = Read-Host "Opcion"

    switch ($choice) {
        "1" { Enable-LocalModels }
        "2" { Disable-LocalModels }
        "3" { Show-Status }
        "4" { break }
        default { Write-Host "Opcion no valida" -ForegroundColor Yellow }
    }
}
