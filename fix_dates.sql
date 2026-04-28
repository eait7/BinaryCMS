UPDATE pages SET created_at = replace(created_at, ' ', 'T') WHERE created_at NOT LIKE '%T%';
UPDATE pages SET updated_at = replace(updated_at, ' ', 'T') WHERE updated_at NOT LIKE '%T%';
UPDATE posts SET created_at = replace(created_at, ' ', 'T') WHERE created_at NOT LIKE '%T%';
UPDATE posts SET updated_at = replace(updated_at, ' ', 'T') WHERE updated_at NOT LIKE '%T%';
UPDATE posts SET published_at = replace(published_at, ' ', 'T') WHERE published_at NOT LIKE '%T%';
