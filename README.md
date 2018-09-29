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
```

TODO
----

- Fire an event when the number of matched logs within the specified time exceeds the specified number
