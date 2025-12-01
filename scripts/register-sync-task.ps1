param(
    [string]$TaskName = "TimeTrackerSync",
    [string]$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path,
    [string]$ExePath,
    [string]$ConfigPath,
    [string]$LogPath,
    [int]$IntervalHours = 2,
    [int]$StartDelayMinutes = 5,
    [switch]$DryRun
)

function Resolve-DefaultPaths {
    if (-not $script:ExePath) {
        $script:ExePath = Join-Path $ProjectRoot "time-tracker-bot.exe"
    }
    if (-not $script:ConfigPath) {
        $script:ConfigPath = Join-Path $ProjectRoot "config.yaml"
    }
    if (-not $script:LogPath) {
        $script:LogPath = Join-Path $ProjectRoot "logs\scheduler-sync.log"
    }

    foreach ($path in @($ExePath, $ConfigPath)) {
        if (-not (Test-Path $path)) {
            throw "Path not found: $path"
        }
    }

    $logDir = Split-Path $LogPath -Parent
    if (-not (Test-Path $logDir)) {
        New-Item -ItemType Directory -Path $logDir | Out-Null
    }
}

function Build-Action {
    $quotedConfig = '"' + $ConfigPath + '"'
    $quotedLog = '"' + $LogPath + '"'
    $args = "sync --config $quotedConfig --tee-output $quotedLog"
    return New-ScheduledTaskAction `
        -Execute $ExePath `
        -Argument $args `
        -WorkingDirectory $ProjectRoot
}

function Build-Trigger {
    $startTime = (Get-Date).AddMinutes($StartDelayMinutes)
    $interval = New-TimeSpan -Hours $IntervalHours
    # Windows Task Scheduler требует конечную длительность повторений.
    # 10 лет (3650 дней) ≈ бесконечность для нашего сценария и не вызывает ошибок.
    $duration = New-TimeSpan -Days 3650
    return New-ScheduledTaskTrigger `
        -Once `
        -At $startTime `
        -RepetitionInterval $interval `
        -RepetitionDuration $duration
}

function Build-Settings {
    return New-ScheduledTaskSettingsSet `
        -Hidden `
        -StartWhenAvailable `
        -MultipleInstances IgnoreNew `
        -ExecutionTimeLimit (New-TimeSpan -Minutes 30)
}

function Register-SyncTask {
    param(
        [Microsoft.Management.Infrastructure.CimInstance]$Action,
        [Microsoft.Management.Infrastructure.CimInstance]$Trigger,
        [Microsoft.Management.Infrastructure.CimInstance]$Settings
    )

    $existing = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($existing) {
        Write-Host "Task '$TaskName' already exists. Removing…" -ForegroundColor Yellow
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
    }

    Register-ScheduledTask `
        -TaskName $TaskName `
        -Action $Action `
        -Trigger $Trigger `
        -Settings $Settings `
        -RunLevel Highest `
        -User "SYSTEM"
}

try {
    Resolve-DefaultPaths
    $action = Build-Action
    $trigger = Build-Trigger
    $settings = Build-Settings

    if ($DryRun) {
        Write-Host "Dry-run mode: would register task with the following parameters:`n" -ForegroundColor Cyan
        Write-Host ("TaskName : {0}" -f $TaskName)
        Write-Host ("ExePath  : {0}" -f $ExePath)
        Write-Host ("Config   : {0}" -f $ConfigPath)
        Write-Host ("Log file : {0}" -f $LogPath)
        Write-Host ("Interval : every {0} hour(s)" -f $IntervalHours)
        Write-Host ("Start in : {0} minute(s)" -f $StartDelayMinutes)
        return
    }

    Register-SyncTask -Action $action -Trigger $trigger -Settings $settings
    Write-Host "Scheduled task '$TaskName' registered. It will run every $IntervalHours hour(s) under SYSTEM without opening a console window." -ForegroundColor Green
}
catch {
    Write-Error $_
    exit 1
}

