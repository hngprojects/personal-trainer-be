-- +goose Up
INSERT INTO roles (name)
VALUES ('customer_care')
ON CONFLICT (name) DO NOTHING;

-- +goose Down
DELETE FROM roles WHERE name = 'customer_care';
