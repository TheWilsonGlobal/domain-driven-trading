# ==============================================================================
# scripts/gen-seed-accounts.ps1: print a SEED_ACCOUNTS value for the k6 benchmark
#
#   .\scripts\gen-seed-accounts.ps1 -Count 500
#   $env:SEED_ACCOUNTS = (.\scripts\gen-seed-accounts.ps1 -Count 500)
#
# Format (see internal/app/seed): ID:cash[:SYM=qty][;ID:cash...]. The ids are
# <Prefix>_0 .. <Prefix>_<Count-1>, the same ids scripts/k6_benchmark.js uses
# (ACCOUNT_PREFIX, default K6; ACCOUNTS=<Count>). The maker account, the fee
# collector and the default accounts are NOT included: set SEED_ACCOUNTS to this
# value plus the maker, which -IncludeMaker appends (ACC_INST_MM with APX stock,
# needed for the gateway's initial quotes).
#
# Windows limits one environment variable to 32767 characters, so Count is
# capped at roughly 900. For more accounts, run several gateways or use a
# shorter prefix; the script stops rather than print a value that gets truncated.
# ==============================================================================

param(
    [int]$Count = 100,
    [string]$Prefix = "K6",
    [long]$Cash = 1000000000000,     # 1e12 VND per account
    [long]$Shares = 1000000,         # APX shares per account
    [switch]$IncludeMaker = $true
)

if ($Count -lt 1) { Write-Error "Count must be >= 1"; exit 1 }
if ($Prefix -notmatch '^[A-Za-z0-9_-]{1,40}$') { Write-Error "Prefix must match [A-Za-z0-9_-]{1,40}"; exit 1 }

$items = New-Object System.Collections.Generic.List[string]
if ($IncludeMaker) {
    $items.Add("ACC_INST_MM:20000000000000:APX=4000000000")
}
for ($i = 0; $i -lt $Count; $i++) {
    $items.Add("${Prefix}_${i}:${Cash}:APX=${Shares}")
}
$value = $items -join ";"
if ($value.Length -gt 30000) {
    Write-Error "SEED_ACCOUNTS would be $($value.Length) chars (> 30000, near the Windows 32767 limit); lower -Count"
    exit 1
}
Write-Output $value
