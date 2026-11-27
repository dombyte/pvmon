# Supported devices

| Profile | Device | Transport | Meter blocks (`source`) | Verified |
| --- | --- | --- | --- | --- |
| `solis/s6-hybrid` | Solis S6 hybrid inverter (and RHI) | Modbus TCP/RTU | `pv`, `pv1`–`pv4`, `battery`, `grid`, `backup` | yes |
| `shelly/pro-3em` | Shelly Pro 3EM | HTTP (Gen2) | `total`, phases `a`, `b`, `c` | yes |
| `shelly/3em` | Shelly 3EM | HTTP (Gen1) | `total`, phases `l1`–`l3` | no |
| `eastron/sdm630` | Eastron SDM630 | Modbus TCP/RTU | `total`, phases `l1`–`l3` | no |
| `goe/charger-v2` | go-eCharger (API v2) | HTTP | `charger` | yes |
| `tinkerforge/warp3` | Tinkerforge WARP3 Charger Pro | HTTP | `charger` | yes |
| `opendtu/total` | OpenDTU (Hoymiles micro inverters) | HTTP | `pv`, `pv_power` | no |

**Verified** shows that the profile agrees with a real device. Profiles that are not
verified have no test on a real device. If you have such a device,
[record its values](/guides/write-a-profile#record-the-values-of-a-device) and send the
file to the project.

For OpenDTU, the block `pv` uses the energy counter. The block `pv_power` has only power,
and pvmon calculates the energy (≈).
