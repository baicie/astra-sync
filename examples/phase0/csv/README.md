# CSV File-to-file Example

From the repository root, package the CLI and run the checked-in JobSpec:

```powershell
mvn -pl cli -am package -DskipTests
java -jar cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar run examples/phase0/csv/job.yaml
```

The command creates `examples/phase0/csv/output.csv`. The CSV Sink never overwrites an existing
file, so remove that generated output before running the example again.
