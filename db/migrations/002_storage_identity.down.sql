-- Manual rollback only. Stop writers and take a backup before running.
-- MySQL DDL is not transactional. This script refuses rollback when newer data
-- cannot be represented losslessly by the legacy global child ID primary key
-- and implicit ID ordering. In that case keep the new schema or restore the
-- pre-upgrade backup; never deduplicate or renumber client IDs to force rollback.
-- The old application must not run until rollback has finished successfully.

DELIMITER $$
DROP PROCEDURE IF EXISTS rollback_storage_identity$$
CREATE PROCEDURE rollback_storage_identity()
BEGIN
  DECLARE duplicates INT DEFAULT 0;
  DECLARE order_changes INT DEFAULT 0;
  DECLARE drops TEXT;

  SELECT COUNT(*) INTO duplicates FROM (
    SELECT id FROM user_packets GROUP BY id HAVING COUNT(*) > 1
  ) AS repeated_ids;
  IF duplicates > 0 THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'Rollback refused: repeated child IDs cannot fit legacy schema; restore backup instead';
  END IF;
  SELECT COUNT(*) INTO order_changes FROM (
    SELECT ordinal, ROW_NUMBER() OVER (PARTITION BY cloud_packet_id ORDER BY id) - 1 AS position
    FROM user_packets
  ) AS ordered WHERE ordinal <> position;
  IF order_changes > 0 THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'Rollback refused: legacy ordering would change child order; restore backup instead';
  END IF;

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
  ALTER TABLE cloud_packets MODIFY COLUMN id INT NOT NULL;
  ALTER TABLE user_packets
    DROP PRIMARY KEY, ADD PRIMARY KEY (id), DROP COLUMN ordinal;
  ALTER TABLE user_packets
    ADD CONSTRAINT fk_user_packets_cloud_packet_id
    FOREIGN KEY (cloud_packet_id) REFERENCES cloud_packets(id)
    ON DELETE CASCADE ON UPDATE CASCADE;
END$$
CALL rollback_storage_identity()$$
DROP PROCEDURE rollback_storage_identity$$
DELIMITER ;
