load-testing
============

See example_payload.json for an example configuration of payloads/urls.

Simple usage:

```
$ ./load_tester -c=100 -n=10000000 example_payload.json
```

Full usage:

```
Usage: ./load_tester [options] CONFIG_FILE

Options:
  -c=10: Concurrency of the load tester
  -n=1000: The number of requests to make
  -v=false: Enable verbose mode
```