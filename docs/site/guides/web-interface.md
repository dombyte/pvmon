# Web interface

The interface shows only the devices and values of your site. It is available in English
and German, with a light theme and a dark theme.

| Page | Content |
| --- | --- |
| Overview | Live energy flow, key values, energy of today, month, year and total, trend of 14 days |
| Day view | Power of one day in intervals of 15 minutes |
| History | Bars for days, months or years in a range that you select |
| Reports | Preview of a period, comparison, download as PDF or CSV |
| Devices | State, profile, live values and status history of each device |
| Status | Status of all devices |
| Settings | Admin only: devices, meters, settings, users, API tokens, audit log |

## Reports

- A report uses local days. The maximum range is 3660 days.
- If the range ends today, the report ends yesterday.
- You can compare with the previous period or with the same period of the last year.
- The comparison uses only the days that have data in the two periods.
- The mark ≈ shows an estimate.

## Status

The profile gives each device code the level **ok**, **notice** or **fault**. The browser
can show a notification for a new fault. pvmon must be open, but not in focus. Set the
notifications to on in each browser.

## Installation as an app

You can install the interface as a progressive web app (PWA). Offline use and the update
message are available only with HTTPS or on `localhost`.

After an update of the server, the interface shows a message. Reload the page.
