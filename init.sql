-- ============================================================================
-- FILE: init.sql
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================================
-- TABLES
-- ============================================================================

CREATE TABLE IF NOT EXISTS players (
    client_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    balance NUMERIC(15, 2) NOT NULL DEFAULT 0.00,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT players_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE IF NOT EXISTS game_session
(
    session_id   UUID PRIMARY KEY        DEFAULT uuid_generate_v4(),
    client_id    UUID           NOT NULL,
    bet_amount   NUMERIC(15, 2) NOT NULL,
    bet_type     TEXT           NOT NULL,
    drawn_number INT,
    result       TEXT,
    pending_win  NUMERIC(15, 2),
    state        TEXT           NOT NULL DEFAULT 'active',
    created_at   TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    closed_at    TIMESTAMPTZ,

    CONSTRAINT fk_game_session_player
    FOREIGN KEY (client_id) REFERENCES players (client_id),
    CONSTRAINT game_session_bet_amount_positive CHECK (bet_amount > 0),
    CONSTRAINT game_session_bet_type_valid CHECK (bet_type IN ('even', 'odd')),
    CONSTRAINT game_session_state_valid CHECK (state IN ('active', 'closed')),
    CONSTRAINT game_session_result_valid CHECK (result IS NULL OR result IN ('win', 'lose')),
    CONSTRAINT game_session_pending_win_non_negative
        CHECK (pending_win IS NULL OR pending_win >= 0),
    CONSTRAINT game_session_closed_at_consistent
        CHECK ((state = 'active' AND closed_at IS NULL)
            OR (state = 'closed' AND closed_at IS NOT NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_game_session_client_state
    ON game_session (client_id, state)
    WHERE (state = 'active');

CREATE TABLE IF NOT EXISTS transactions (
    transaction_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    related_session_id UUID,
    client_id UUID NOT NULL,
    amount NUMERIC(15,2) NOT NULL,
    type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_transaction_player
        FOREIGN KEY (client_id) REFERENCES players (client_id),
    CONSTRAINT fk_transaction_game_session
        FOREIGN KEY (related_session_id) REFERENCES game_session (session_id)
);

CREATE INDEX IF NOT EXISTS idx_transactions_client_created_at
    ON transactions (client_id, created_at);
