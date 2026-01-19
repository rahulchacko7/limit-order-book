CREATE TABLE IF NOT EXISTS currency_pairs (
    base VARCHAR(10) NOT NULL,
    quote VARCHAR(10) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (base, quote),
    FOREIGN KEY (base) REFERENCES currencies(code),
    FOREIGN KEY (quote) REFERENCES currencies(code)
);

CREATE INDEX IF NOT EXISTS idx_pairs_active ON currency_pairs(is_active) WHERE is_active = true;

