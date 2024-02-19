-- asset_sell_offers is a table that stores the set of all asset sell offers
-- that are applicable to the RFQ negotiation process.
CREATE TABLE IF NOT EXISTS asset_sell_offers (
    txn_id BIGINT PRIMARY KEY,

    asset_id BIGINT REFERENCES assets(asset_id),

    max_amount BIGINT NOT NULL
);
