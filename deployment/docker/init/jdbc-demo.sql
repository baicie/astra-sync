CREATE TABLE source_data (
    id INTEGER PRIMARY KEY,
    name VARCHAR(64) NOT NULL
);

CREATE TABLE target_data (
    id INTEGER PRIMARY KEY,
    name VARCHAR(64) NOT NULL
);

INSERT INTO source_data (id, name) VALUES
    (1, 'Ada'),
    (2, 'Lin'),
    (3, 'Kai'),
    (4, 'May');
