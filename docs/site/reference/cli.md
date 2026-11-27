# Command line

| Command | Function | pvmon must be stopped |
| --- | --- | --- |
| `pvmon`, `pvmon serve` | Start the server | |
| `pvmon version` | Show the version | no |
| `pvmon config validate` | Examine the configuration | no |
| `pvmon config export` | Print the configuration as YAML, without secrets | no |
| `pvmon user add <name> [--lang de\|en]` | Add an admin account | yes |
| `pvmon user reset-password <name>` | Set a new password | yes |
| `pvmon user list` | List the accounts | yes |
| `pvmon token create --name <name> [--expires 90d]` | Create an API token | yes |
| `pvmon token list` | List the tokens | yes |
| `pvmon token revoke <id>` | Revoke a token | yes |
| `pvmon profile validate <file>` | Examine profile files | no |
| `pvmon dump --device <id> [--count N] [--interval D] [-o file]` | Record the values of a device | yes |
| `pvmon import-solis --db <file> --map <file>` | [Import a solis database](/guides/import-solis) | yes |
| `pvmon simulate [--setup R1..R7]` | Start with simulated devices | |

The option `-config <path>` selects the configuration file. The default is `config.yaml`.

`user add` and `user reset-password` read the password from the first line of the standard
input.

| Exit code | Description |
| --- | --- |
| 0 | Success |
| 1 | Failure |
| 2 | Incorrect command line |
