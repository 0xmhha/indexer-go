-- Add the non-transaction pre-block cause without rewriting stored journals.
-- ALTER on each partitioned parent also updates existing history partitions.
ALTER TABLE state_account_changes DROP CONSTRAINT state_account_changes_cause_check;
ALTER TABLE state_account_changes ADD CONSTRAINT state_account_changes_cause_check CHECK(cause BETWEEN 0 AND 4);
ALTER TABLE state_storage_changes DROP CONSTRAINT state_storage_changes_cause_check;
ALTER TABLE state_storage_changes ADD CONSTRAINT state_storage_changes_cause_check CHECK(cause BETWEEN 0 AND 4);
ALTER TABLE state_storage_clears DROP CONSTRAINT state_storage_clears_cause_check;
ALTER TABLE state_storage_clears ADD CONSTRAINT state_storage_clears_cause_check CHECK(cause BETWEEN 0 AND 4);
ALTER TABLE state_contract_changes DROP CONSTRAINT state_contract_changes_cause_check;
ALTER TABLE state_contract_changes ADD CONSTRAINT state_contract_changes_cause_check CHECK(cause BETWEEN 0 AND 4);
