-- Fix unique constraints to allow soft-deleted records with same name/alias
-- MySQL doesn't support partial indexes, so we use generated columns

-- 1. Drop existing unique constraints on projects
DROP INDEX `name` ON projects;
DROP INDEX `idx_projects_name` ON projects;
DROP INDEX `idx_projects_alias` ON projects;

-- 2. Add generated columns that are NULL when record is soft-deleted
ALTER TABLE projects ADD COLUMN `unique_name` VARCHAR(255) GENERATED ALWAYS AS (
    CASE WHEN deleted_at IS NULL THEN name ELSE NULL END
) STORED;

ALTER TABLE projects ADD COLUMN `unique_alias` VARCHAR(255) GENERATED ALWAYS AS (
    CASE WHEN deleted_at IS NULL THEN alias ELSE NULL END
) STORED;

-- 3. Create unique indexes on generated columns (NULL values are ignored in unique constraints)
CREATE UNIQUE INDEX `idx_projects_unique_name` ON projects (`unique_name`);
CREATE UNIQUE INDEX `idx_projects_unique_alias` ON projects (`unique_alias`);

-- 4. Apply same fix to data_types table
DROP INDEX `idx_data_types_name` ON data_types;
DROP INDEX `idx_datatype_project_name` ON data_types;

ALTER TABLE data_types ADD COLUMN `unique_name` VARCHAR(255) GENERATED ALWAYS AS (
    CASE WHEN deleted_at IS NULL THEN name ELSE NULL END
) STORED;

CREATE UNIQUE INDEX `idx_data_types_unique_name` ON data_types (`project_id`, `unique_name`);
