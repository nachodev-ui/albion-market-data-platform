[CmdletBinding()]
param(
    [string]$OutputPath = "",
    [string]$RepositoryRoot = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Invoke-Git {
    param([string]$Root, [string[]]$Arguments)

    $output = & git -C $Root @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "git $($Arguments -join ' ') failed:`n$($output -join [Environment]::NewLine)"
    }
    return @($output)
}

function Normalize-Path {
    param([string]$Path)
    return ($Path -replace '\\', '/').TrimStart([char]'/')
}

function Test-ForbiddenPath {
    param([string]$Path)

    $normalized = Normalize-Path $Path
    $patterns = @(
        '(^|/)\.git(/|$)',
        '(^|/)\.env($|\.)',
        '^secrets/',
        '^\.e2e/',
        '^bin/',
        '(^|/)[^/]*\.(exe|test|token|secret)$',
        '^albiondata-client\.log(?:\.|$)',
        '^data/.*\.(json|jsonl)$',
        '^data/normalized-backup-/'
    )
    foreach ($pattern in $patterns) {
        if ($normalized -match $pattern) {
            return $true
        }
    }
    return $false
}

if ([string]::IsNullOrWhiteSpace($RepositoryRoot)) {
    $RepositoryRoot = (Invoke-Git $PSScriptRoot @('rev-parse', '--show-toplevel') | Select-Object -First 1).Trim()
}
$RepositoryRoot = [System.IO.Path]::GetFullPath($RepositoryRoot)

if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    $OutputPath = Join-Path $RepositoryRoot 'artifacts/albion-market-data-platform-source.zip'
} elseif (-not [System.IO.Path]::IsPathRooted($OutputPath)) {
    $OutputPath = Join-Path $RepositoryRoot $OutputPath
}
$OutputPath = [System.IO.Path]::GetFullPath($OutputPath)

$trackedFiles = @(Invoke-Git $RepositoryRoot @('ls-files', '--cached') |
    ForEach-Object { Normalize-Path ([string]$_) } |
    Where-Object { $_ -ne '' } |
    Sort-Object -Unique)
if ($trackedFiles.Count -eq 0) {
    throw 'The repository has no tracked files to export.'
}

$forbidden = @($trackedFiles | Where-Object { Test-ForbiddenPath $_ })
if ($forbidden.Count -gt 0) {
    throw "Forbidden runtime, secret, binary, or repository files are tracked:`n$($forbidden -join [Environment]::NewLine)"
}

$secretPatterns = @(
    '-----BEGIN ([A-Z ]+ )?PRIVATE KEY-----',
    ('gh' + '[pousr]_[A-Za-z0-9]{36,}'),
    ('github' + '_pat_[A-Za-z0-9_]{60,}'),
    ('AK' + 'IA[0-9A-Z]{16}')
)
$textExtensions = @('.conf', '.env', '.go', '.json', '.jsonl', '.md', '.mod', '.ps1', '.sql', '.sum', '.txt', '.yaml', '.yml')
$findings = [System.Collections.Generic.List[string]]::new()
foreach ($relativePath in $trackedFiles) {
    $extension = [System.IO.Path]::GetExtension($relativePath).ToLowerInvariant()
    if ($textExtensions -notcontains $extension -and -not $relativePath.StartsWith('.env', [System.StringComparison]::OrdinalIgnoreCase)) {
        continue
    }

    $sourcePath = Join-Path $RepositoryRoot ($relativePath -replace '/', [System.IO.Path]::DirectorySeparatorChar)
    if (-not (Test-Path -LiteralPath $sourcePath -PathType Leaf)) {
        throw "Tracked file is missing or is not a regular file: $relativePath"
    }
    $content = [System.IO.File]::ReadAllText($sourcePath)
    foreach ($pattern in $secretPatterns) {
        if ([System.Text.RegularExpressions.Regex]::IsMatch($content, $pattern)) {
            $findings.Add($relativePath)
            break
        }
    }
}
if ($findings.Count -gt 0) {
    throw "Potential secrets were found in tracked source files:`n$($findings -join [Environment]::NewLine)"
}

$outputDirectory = Split-Path -Parent $OutputPath
[System.IO.Directory]::CreateDirectory($outputDirectory) | Out-Null
if (Test-Path -LiteralPath $OutputPath) {
    Remove-Item -LiteralPath $OutputPath -Force
}

Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
$timestampText = (Invoke-Git $RepositoryRoot @('show', '-s', '--format=%cI', 'HEAD') | Select-Object -First 1).Trim()
$timestamp = [DateTimeOffset]::Parse($timestampText, [System.Globalization.CultureInfo]::InvariantCulture)

$fileStream = [System.IO.File]::Open($OutputPath, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::ReadWrite, [System.IO.FileShare]::None)
try {
    $archive = [System.IO.Compression.ZipArchive]::new($fileStream, [System.IO.Compression.ZipArchiveMode]::Create, $false)
    try {
        foreach ($relativePath in $trackedFiles) {
            $sourcePath = Join-Path $RepositoryRoot ($relativePath -replace '/', [System.IO.Path]::DirectorySeparatorChar)
            $entry = $archive.CreateEntry($relativePath, [System.IO.Compression.CompressionLevel]::Optimal)
            $entry.LastWriteTime = $timestamp
            $entryStream = $entry.Open()
            try {
                $sourceStream = [System.IO.File]::OpenRead($sourcePath)
                try { $sourceStream.CopyTo($entryStream) } finally { $sourceStream.Dispose() }
            } finally {
                $entryStream.Dispose()
            }
        }
    } finally {
        $archive.Dispose()
    }
} finally {
    $fileStream.Dispose()
}

$readStream = [System.IO.File]::OpenRead($OutputPath)
try {
    $archive = [System.IO.Compression.ZipArchive]::new($readStream, [System.IO.Compression.ZipArchiveMode]::Read, $false)
    try {
        $entries = @($archive.Entries |
            Where-Object { -not $_.FullName.EndsWith('/') } |
            ForEach-Object { Normalize-Path $_.FullName } |
            Sort-Object -Unique)
    } finally {
        $archive.Dispose()
    }
} finally {
    $readStream.Dispose()
}

$missing = @($trackedFiles | Where-Object { $entries -notcontains $_ })
$unexpected = @($entries | Where-Object { $trackedFiles -notcontains $_ -or (Test-ForbiddenPath $_) })
if ($missing.Count -gt 0 -or $unexpected.Count -gt 0) {
    throw "Archive verification failed. Missing=$($missing.Count), Unexpected=$($unexpected.Count)"
}

$hash = (Get-FileHash -LiteralPath $OutputPath -Algorithm SHA256).Hash.ToLowerInvariant()
Write-Host "Source package created and verified."
Write-Host "Archive=$OutputPath"
Write-Host "TrackedFiles=$($trackedFiles.Count)"
Write-Host "SHA256=$hash"
