$ErrorActionPreference = "Stop"
$benchmarkArguments = $args
$repositoryRoot = Resolve-Path (Join-Path $PSScriptRoot "../..")
Push-Location $repositoryRoot
try {
    & mvn -B -ntp -pl tests/benchmark -am package -DskipTests
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
    & java --add-opens=java.base/java.nio=ALL-UNNAMED `
        -jar tests/benchmark/target/astrasync-benchmarks.jar @benchmarkArguments
    exit $LASTEXITCODE
} finally {
    Pop-Location
}
