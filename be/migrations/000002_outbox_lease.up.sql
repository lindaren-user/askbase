ALTER TABLE t_outbox
    ADD COLUMN IF NOT EXISTS lease_token UUID,
    ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ;

COMMENT ON COLUMN t_outbox.status IS '发布状态：pending、processing 或 published';
COMMENT ON COLUMN t_outbox.lease_token IS '当前 relay 领取事件的唯一标识';
COMMENT ON COLUMN t_outbox.lease_until IS '当前领取租约的过期时间';

CREATE INDEX IF NOT EXISTS idx_t_outbox_processing_lease
    ON t_outbox (lease_until, id)
    WHERE status = 'processing';
