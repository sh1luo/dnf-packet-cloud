-- MySQL 8.0+. Select the target database before sourcing this migration.
-- Stop all writers and take a database-native backup first: MySQL DDL commits
-- implicitly. Keep 001_init.sql as the historical schema; new installations
-- may use schema.sql directly. This upgrade is restartable after partial DDL.
-- Legacy SQL did not store child order, so existing children are ordered by ID.
-- New writes retain the exact protobuf repeated-field order using ordinal.
-- No packet or child payload is deleted or renumbered.

DELIMITER $$
DROP PROCEDURE IF EXISTS migrate_storage_identity$$
CREATE PROCEDURE migrate_storage_identity()
BEGIN
  DECLARE ordinal_exists INT DEFAULT 0;
  DECLARE desired_pk INT DEFAULT 0;
  DECLARE drops TEXT;

  IF DATABASE() IS NULL THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Select the target database first';
  END IF;

  SELECT COUNT(*) INTO ordinal_exists FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_packets' AND COLUMN_NAME = 'ordinal';
  SELECT COUNT(*) INTO desired_pk FROM information_schema.KEY_COLUMN_USAGE
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_packets'
      AND CONSTRAINT_NAME = 'PRIMARY' AND COLUMN_NAME = 'ordinal';

  IF ordinal_exists = 0 THEN
    ALTER TABLE user_packets ADD COLUMN ordinal INT NULL AFTER cloud_packet_id;
  END IF;
  IF desired_pk = 0 THEN
    -- Window functions force materialization, avoiding a target-table update
    -- dependency. Legacy id is globally unique, including any explicit zero.
    UPDATE user_packets AS target
    JOIN (
      SELECT id, ROW_NUMBER() OVER (PARTITION BY cloud_packet_id ORDER BY id) - 1 AS position
      FROM user_packets
    ) AS ordered ON ordered.id = target.id
    SET target.ordinal = ordered.position;
    ALTER TABLE user_packets
      MODIFY COLUMN id INT NOT NULL,
      MODIFY COLUMN ordinal INT NOT NULL,
      DROP PRIMARY KEY,
      ADD PRIMARY KEY (cloud_packet_id, ordinal);
  END IF;

  -- Both historical SQL and GORM installations have used different FK names.
  -- Temporarily drop only the child-to-parent constraints before changing id.
  SELECT GROUP_CONCAT(CONCAT('DROP FOREIGN KEY `', REPLACE(CONSTRAINT_NAME, '`', '``'), '`') SEPARATOR ', ')
    INTO drops
    FROM information_schema.REFERENTIAL_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = 'user_packets'
      AND REFERENCED_TABLE_NAME = 'cloud_packets';
  IF drops IS NOT NULL THEN
    SET @storage_ddl = CONCAT('ALTER TABLE user_packets ', drops);
    PREPARE storage_statement FROM @storage_ddl;
    EXECUTE storage_statement;
    DEALLOCATE PREPARE storage_statement;
  END IF;

  ALTER TABLE cloud_packets MODIFY COLUMN id INT NOT NULL AUTO_INCREMENT;
  ALTER TABLE user_packets
    ADD CONSTRAINT fk_user_packets_cloud_packet_id
    FOREIGN KEY (cloud_packet_id) REFERENCES cloud_packets(id)
    ON DELETE CASCADE ON UPDATE CASCADE;
END$$
CALL migrate_storage_identity()$$
DROP PROCEDURE migrate_storage_identity$$
DELIMITER ;
