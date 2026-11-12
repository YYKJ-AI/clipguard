param(
    [int]$Port = 8090,
    [switch]$Online,
    [string]$ModelBaseURL = '',
    [string]$Model = ''
)
$ErrorActionPreference = 'Stop'
if ($Port -lt 1 -or $Port -gt 65535) { throw '端口必须在 1—65535 之间。' }
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectRoot
$savedSettings = @{}
foreach ($name in @('LISTEN_ADDR','LLM_BASE_URL','LLM_MODEL','LLM_API_KEY','LLM_JSON_MODE','LLM_THINKING','LLM_MAX_TOKENS','PRODUCT_AUTH_FILE')) {
    $savedSettings[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
try {
    $env:LISTEN_ADDR = "127.0.0.1:$Port"
    Remove-Item Env:PRODUCT_AUTH_FILE -ErrorAction SilentlyContinue
    if ($Online) {
        if ($ModelBaseURL) { $env:LLM_BASE_URL = $ModelBaseURL }
        if ($Model) { $env:LLM_MODEL = $Model }
        if (!$env:LLM_BASE_URL -or !$env:LLM_MODEL) { throw '在线模式需要提供 ModelBaseURL 和 Model，或设置对应 LLM 环境变量。' }
        if (!$env:LLM_API_KEY) {
            $privateKey = Read-Host '请输入你自己的模型密钥（输入不显示，不保存到文件）' -AsSecureString
            $privatePointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($privateKey)
            try { $env:LLM_API_KEY = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($privatePointer) }
            finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($privatePointer); $privateKey.Dispose() }
        }
    } else {
        foreach ($name in @('LLM_BASE_URL','LLM_MODEL','LLM_API_KEY','LLM_JSON_MODE','LLM_THINKING','LLM_MAX_TOKENS')) {
            [Environment]::SetEnvironmentVariable($name, $null, 'Process')
        }
    }
    New-Item -ItemType Directory -Force -Path 'bin' | Out-Null
    & go build -trimpath -o bin/safety-edit-agent.exe .
    if ($LASTEXITCODE -ne 0) { throw '构建失败。请安装 Go 1.22 或更新版本，并检查上面的输出。' }
    Write-Host "请在浏览器打开 http://127.0.0.1:$Port ，按 Ctrl+C 停止。"
    & '.\bin\safety-edit-agent.exe'
    if ($LASTEXITCODE -ne 0) { throw '程序未能正常运行，请检查端口或模型配置。' }
} finally {
    foreach ($name in $savedSettings.Keys) { [Environment]::SetEnvironmentVariable($name, $savedSettings[$name], 'Process') }
    Pop-Location
}
