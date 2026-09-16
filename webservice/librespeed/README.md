# LibreSpeed frontend

Source: https://github.com/librespeed/speedtest
Pinned commit: 22b89a76c59752085151982fe34034918e50c048
Version reported by upstream: 6.2.1

`speedtest.js` and `speedtest_worker.js` are unmodified upstream sources by Federico Dossena, distributed under GNU LGPLv3. See LICENSE and COPYING. These readable sources are embedded and served to browsers by DWS. Users can replace them and rebuild dnet with `go build .`.

DWS supplies its own page and Go HTTP endpoints. It disables telemetry and IP/ISP lookup, runs ping/jitter, download and upload against the same origin, and uses a 1.0 overhead factor to report application throughput. No external scripts or test servers are loaded.
