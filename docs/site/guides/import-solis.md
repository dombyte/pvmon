# Import from solis

You can import the history of solis into pvmon. solis is an older monitor for Solis
inverters.

1. Stop pvmon.
2. Copy the file
   [import-solis.map.yaml](https://github.com/dombyte/pvmon/blob/main/example/import-solis.map.yaml).
3. Change the copy to agree with your meters.
4. Do the import:

   ```bash
   pvmon import-solis --db solis.db --map import-solis.map.yaml
   ```

5. Read the summary. It shows each series, the days that pvmon did not import and all
   differences.

## Rules of the import

- pvmon does not change the solis database.
- Before the import, pvmon makes a backup of its own database.
- pvmon imports only the days before its own first data.
- You can do only one import into a database.
- pvmon marks the imported values as estimates, because solis supplies daily values.
- pvmon does not import consumption values. It calculates the consumption.
