# First start

1. Start pvmon.
2. Find the setup code in the log. It is the value of the field `setup_code`.
3. Open the web interface.
4. Enter the setup code.
5. Create the first admin account.

A password must have a minimum of 12 characters. pvmon does not accept common passwords.

The setup code has 8 characters. You can use it one time. If pvmon starts again before you
use it, find the new code in the log.

## Setup wizard

If the configuration has no meters, the setup wizard starts.

1. **Devices:** add each device.
2. Enter the address of the device.
3. Select a profile.
4. Select **Test** to read the device one time.
5. If a Modbus device does not answer, select **Try unit IDs**.
6. **Meters:** switch on the meters that you use.
7. **Hierarchy:** set the category and the parent of each consumer.
8. **Check:** compare the values with the displays of your devices.
9. **Save:** save the configuration.

The wizard cannot change the direction of a meter. It does not examine
[grid netting](/concepts/netting).

## Admin account from the command line

1. Stop pvmon.
2. Add the account:

   ```bash
   printf '%s\n' "$PASSWORD" | pvmon user add admin
   ```

## State of the system

- The page **Devices** shows the state of each device.
- `GET /health` gives the status 200 if pvmon operates. It gives 503 if a necessary part
  failed.
