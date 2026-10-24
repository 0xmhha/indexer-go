# StableNet spool import compatibility

This package aliases the [shared durable execution spool](../../../../execwire/spool/README.md). Existing StableNet assembly and on-disk spool formats are unchanged. Implementation, crash-recovery and concurrency tests now live in `execwire/spool`; native StableNet retention remains chain-specific.
