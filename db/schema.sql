CREATE DATABASE IF NOT EXISTS `packet_cloud` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
USE `packet_cloud`;

CREATE TABLE IF NOT EXISTS `cloud_packets` (
  `id` INT NOT NULL,
  `region` VARCHAR(32) NOT NULL,
  `name` VARCHAR(64) NOT NULL,
  `channel` VARCHAR(32) NOT NULL,
  `uploader` VARCHAR(64) NOT NULL,
  `time` VARCHAR(32) NOT NULL,
  PRIMARY KEY (`id`),
  INDEX `idx_time` (`time`),
  INDEX `idx_uploader` (`uploader`),
  INDEX `idx_region` (`region`),
  INDEX `idx_channel` (`channel`),
  INDEX `idx_uploader_time` (`uploader`,`time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `user_packets` (
  `id` INT NOT NULL,
  `cloud_packet_id` INT NOT NULL,
  `name` VARCHAR(64) NOT NULL,
  `content` LONGTEXT NOT NULL,
  `size` INT NOT NULL,
  `send_timing` VARCHAR(32) NOT NULL,
  PRIMARY KEY (`id`),
  INDEX `idx_cloud_packet_id` (`cloud_packet_id`),
  CONSTRAINT `fk_user_packets_cloud_packet_id` FOREIGN KEY (`cloud_packet_id`) REFERENCES `cloud_packets`(`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;