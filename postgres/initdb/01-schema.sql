CREATE TABLE users (
  id   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name TEXT NOT NULL
);

CREATE TABLE products (
  id    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name  TEXT NOT NULL,
  price NUMERIC(10,2) NOT NULL
);

CREATE TABLE orders (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  product_id BIGINT NOT NULL REFERENCES products(id),
  qty        INT NOT NULL CHECK (qty > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO users (name) VALUES ('Ada'), ('Alan'), ('Grace');
INSERT INTO products (name, price) VALUES ('Widget', 9.99), ('Gadget', 19.99), ('Gizmo', 4.50);

