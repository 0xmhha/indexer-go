-- Offline collection is fenced durably, across lost connections/process death.
-- A single retained completion makes exact retries inspectable without an
-- unbounded GC history. The coordinator, not feature undo, owns this record.
CREATE TABLE exec_retention (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 token bytea NOT NULL CHECK(octet_length(token)=32),
 permit bytea NOT NULL CHECK(octet_length(permit) BETWEEN 1 AND 65536),
 pending boolean NOT NULL,
 receipt bytea CHECK(receipt IS NULL OR octet_length(receipt) BETWEEN 1 AND 65536),
 CHECK(pending OR receipt IS NOT NULL)
);
