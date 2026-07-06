-- Focused excerpt in the style of the MySQL Sakila sample database — trimmed
-- to a few tables that exercise PK, FK, an index, an ENUM column, backtick
-- identifiers, and an UNSIGNED/AUTO_INCREMENT column. Not the whole schema;
-- assembled for this test only.
--   Reference: https://dev.mysql.com/doc/sakila/en/ (sakila-schema.sql)

CREATE TABLE `actor` (
  `actor_id` SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `first_name` VARCHAR(45) NOT NULL,
  `last_name` VARCHAR(45) NOT NULL,
  `last_update` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`actor_id`)
);

CREATE INDEX `idx_actor_last_name` ON `actor` (`last_name`);

CREATE TABLE `film` (
  `film_id` SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `title` VARCHAR(255) NOT NULL,
  `description` TEXT,
  `release_year` YEAR,
  `rating` ENUM('G','PG','PG-13','R','NC-17') DEFAULT 'G',
  `special_features` SET('Trailers','Commentaries','Deleted Scenes','Behind the Scenes'),
  PRIMARY KEY (`film_id`)
);

CREATE TABLE `film_actor` (
  `actor_id` SMALLINT UNSIGNED NOT NULL,
  `film_id` SMALLINT UNSIGNED NOT NULL,
  `last_update` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`actor_id`, `film_id`),
  CONSTRAINT `fk_film_actor_actor` FOREIGN KEY (`actor_id`) REFERENCES `actor` (`actor_id`),
  CONSTRAINT `fk_film_actor_film` FOREIGN KEY (`film_id`) REFERENCES `film` (`film_id`)
);

CREATE INDEX `idx_fk_film_id` ON `film_actor` (`film_id`);

-- Historical note (kept at end-of-file so it does not shift statement line
-- numbers in the golden snapshot): earlier revisions of this parser mis-parsed
-- MySQL's inline `KEY name (cols)` secondary-index shorthand inside CREATE
-- TABLE into a phantom column named "KEY" and silently dropped the index.
-- Unit I (task I4b) fixed this — inline KEY/INDEX/FULLTEXT KEY/SPATIAL KEY is
-- now recorded as a table index, never a column (see
-- internal/providers/sqlddl/limits_test.go). This fixture intentionally keeps
-- using standalone CREATE INDEX so the golden snapshot above stays
-- byte-identical; the inline-shorthand behavior is locked by its own test.
