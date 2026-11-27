# Write a device profile

A profile is a YAML file that describes one device model. It supports Modbus TCP, Modbus
RTU and HTTP with JSON.

1. Copy a [built-in profile](https://github.com/dombyte/pvmon/tree/main/profiles) that uses
   the same transport.
2. Change the blocks and channels. Refer to the [profile schema](/reference/profiles).
3. Examine the file:

   ```bash
   pvmon profile validate ./profiles/my-meter.yaml
   ```

4. Set the path of the file in the device:

   ```yaml
   devices:
     meter: { address: "tcp://10.0.0.50:502", unit: 2, profile: ./profiles/my-meter.yaml }
   ```

5. Compare the values on the page **Devices** with the display of the device.
6. Make sure that the signs agree with the [site model](/concepts/site-model).

The path must end in `.yaml` or `.yml`. In `config.yaml`, a relative path starts at the
directory of that file. In the web interface, it starts at the data directory.

## Record the values of a device

1. Stop pvmon.
2. Record the values:

   ```bash
   pvmon dump --device meter --count 10 -o meter.fixture.json
   ```

The file contains only the values that the profile reads.

## Send a profile to the project

Open a pull request with these items:

- the profile
- a recorded file (fixture)
- the expected values

Set `verified: false` until a recorded file from a real device agrees with the profile.
