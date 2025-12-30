-- QuantMesh 加密货币支付数据库初始化脚本
-- 创建日期: 2025-12-30

-- 创建加密货币支付表
CREATE TABLE IF NOT EXISTS crypto_payments (
    id SERIAL PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL,
    plan VARCHAR(50) NOT NULL,
    amount DECIMAL(10, 2) NOT NULL,
    currency VARCHAR(3) DEFAULT 'USD',
    crypto_currency VARCHAR(10),
    crypto_amount DECIMAL(20, 8),
    payment_method VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL,
    charge_id VARCHAR(255),
    payment_address TEXT,
    transaction_hash VARCHAR(255),
    expires_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_crypto_payments_user_id ON crypto_payments(user_id);
CREATE INDEX IF NOT EXISTS idx_crypto_payments_status ON crypto_payments(status);
CREATE INDEX IF NOT EXISTS idx_crypto_payments_charge_id ON crypto_payments(charge_id);
CREATE INDEX IF NOT EXISTS idx_crypto_payments_expires_at ON crypto_payments(expires_at);
CREATE INDEX IF NOT EXISTS idx_crypto_payments_created_at ON crypto_payments(created_at);

-- 创建更新时间触发器
CREATE OR REPLACE FUNCTION update_crypto_payments_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_crypto_payments_updated_at
    BEFORE UPDATE ON crypto_payments
    FOR EACH ROW
    EXECUTE FUNCTION update_crypto_payments_updated_at();

-- 插入测试数据 (可选)
-- INSERT INTO crypto_payments (
--     user_id, email, plan, amount, currency, crypto_currency, crypto_amount,
--     payment_method, status, payment_address, expires_at
-- ) VALUES (
--     'test_user_1', 'test@example.com', 'professional', 199.00, 'USD',
--     'USDT', 199.0, 'direct', 'pending',
--     '0x1234567890abcdef1234567890abcdef12345678',
--     NOW() + INTERVAL '24 hours'
-- );

-- 查询统计信息
SELECT 
    status,
    COUNT(*) as count,
    SUM(amount) as total_amount
FROM crypto_payments
GROUP BY status;

-- 查看最近的支付
SELECT 
    id, user_id, email, plan, amount, crypto_currency, 
    payment_method, status, created_at
FROM crypto_payments
ORDER BY created_at DESC
LIMIT 10;

