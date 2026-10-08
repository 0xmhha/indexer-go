-- The number of stored transactions, kept by SetBlock from now on so
-- GetTransactionCount does not scan the table (refactoring plan R4-2,
-- stage 6). Big-endian 8 bytes, like the other meta counters.
INSERT INTO meta (name, value)
SELECT 'transaction_count', int8send(count(*)) FROM transactions
ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value;
