gowatch
=======

Gowatch is [swatch](https://sourceforge.net/projects/swatch/) alternative implemented in go.

Config
------

```toml
file_path = "/path/to/some/logfile"
verbose = true

[[rules]]
name = "test1"
regexp_patterns = [
  "(?i)OutOfMemory",
]
patterns = [
  "some text",
  "other text",
]
commands = [
  "mail -s \"test mail\" monitoring@example.com",
]
backoff = 30

[[rules]]
name = "test2"
patterns = [
  "FATAL",
]
commands = [
  "service foobar restart",
]

[[rules]]
name = "test3"
patterns = [
  "WARNING",
]
commands = [
  "service foobar restart",
]
window_sec = 30
max_in_window = 10
backoff = 30
```
