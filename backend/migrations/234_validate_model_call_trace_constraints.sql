-- Validate the compatibility constraints after the additive schema and online
-- indexes are in place.  VALIDATE CONSTRAINT keeps ordinary reads and writes
-- available while PostgreSQL verifies historical rows.
ALTER TABLE model_call_trace_cleanup_runs
    VALIDATE CONSTRAINT chk_model_call_trace_cleanup_runs_nonnegative;
ALTER TABLE model_call_payloads
    VALIDATE CONSTRAINT chk_model_call_payloads_kind;
