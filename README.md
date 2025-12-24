## Output Reference

The output keys are defined by you in `config.yaml`.
Format: `[BROADCAST] <your_key_name>: <value>`

### Available Metric Types

| Config `type` | Config `measure` Options | Value Description |
| :--- | :--- | :--- |
| **`disk`** | `percent_used`, `percent_free`, `used_gb`, `free_gb` | Disk usage for the specific `path` defined in config. |
| **`disk_auto`** | (Same as disk) | Scans all mounts. Keys are auto-generated (e.g., `disk_auto_mnt_data`). |
| **`net_rate`** | `rx_mbps`, `tx_mbps` | Real-time network throughput in Megabits per second. |
| **`service`** | N/A | **1.00** = Active (Running), **0.00** = Inactive/Failed. |
| **`cpu`** | `total`, `per_core` | CPU Load %. If `per_core`, keys are suffixed `_0`, `_1`, etc. |
| **`mem`** | `percent`, `free_gb` | Physical RAM usage. |
| **`swap`** | `percent`, `free_gb` | Swap file/partition usage. |