package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20241122171434, Down_20241122171434)
}

func Up_20241122171434(tx *sql.Tx) error {
	// Idempotent migration.
	// Duplicate indexes identified after running pt-duplicate-key-checker
	// https://docs.percona.com/percona-toolkit/pt-duplicate-key-checker.html

	// # ########################################################################
	// # fleet.app_config_json
	// # ########################################################################
	//
	// # Uniqueness of id ignored because PRIMARY is a duplicate constraint
	// # id is a duplicate of PRIMARY
	// # Key definitions:
	// #   UNIQUE KEY `id` (`id`)
	// #   PRIMARY KEY (`id`),
	// # Column types:
	// #	  `id` int unsigned not null default '1'
	// # To remove this duplicate index, execute:
	// ALTER TABLE `fleet`.`app_config_json` DROP INDEX `id`;
	//
	// # ########################################################################
	// # fleet.host_users
	// # ########################################################################
	//
	// # idx_uid_username is a duplicate of PRIMARY
	// # Key definitions:
	// #   UNIQUE KEY `idx_uid_username` (`host_id`,`uid`,`username`)
	// #   PRIMARY KEY (`host_id`,`uid`,`username`),
	// # Column types:
	// #	  `host_id` int unsigned not null
	// #	  `uid` int unsigned not null
	// #	  `username` varchar(255) collate utf8mb4_unicode_ci not null
	// # To remove this duplicate index, execute:
	// ALTER TABLE `fleet`.`host_users` DROP INDEX `idx_uid_username`;
	//
	// # ########################################################################
	// # fleet.migration_status_tables
	// # ########################################################################
	//
	// # Uniqueness of id ignored because PRIMARY is a duplicate constraint
	// # id is a duplicate of PRIMARY
	// # Key definitions:
	// #   UNIQUE KEY `id` (`id`)
	// #   PRIMARY KEY (`id`),
	// # Column types:
	// #	  `id` bigint unsigned not null auto_increment
	// # To remove this duplicate index, execute:
	// ALTER TABLE `fleet`.`migration_status_tables` DROP INDEX `id`;
	//
	// # ########################################################################
	// # fleet.policy_membership
	// # ########################################################################
	//
	// # idx_policy_membership_policy_id is a left-prefix of PRIMARY
	// # Key definitions:
	// #   KEY `idx_policy_membership_policy_id` (`policy_id`),
	// #   PRIMARY KEY (`policy_id`,`host_id`),
	// # Column types:
	// #	  `policy_id` int unsigned not null
	// #	  `host_id` int unsigned not null
	// # To remove this duplicate index, execute:
	// ALTER TABLE `fleet`.`policy_membership` DROP INDEX `idx_policy_membership_policy_id`;
	//
	// # ########################################################################
	// # fleet.software
	// # ########################################################################
	//
	// # Key software_listing_idx ends with a prefix of the clustered index
	// # Key definitions:
	// #   KEY `software_listing_idx` (`name`,`id`),
	// #   PRIMARY KEY (`id`),
	// # Column types:
	// #	  `name` varchar(255) collate utf8mb4_unicode_ci not null
	// #	  `id` bigint unsigned not null auto_increment
	// # To shorten this duplicate clustered index, execute:
	// ALTER TABLE `fleet`.`software` DROP INDEX `software_listing_idx`, ADD INDEX `software_listing_idx` (`name`);
	//
	// # ########################################################################
	// # fleet.software_cve
	// # ########################################################################
	//
	// # software_cve_software_id is a left-prefix of unq_software_id_cve
	// # Key definitions:
	// #   KEY `software_cve_software_id` (`software_id`)
	// #   UNIQUE KEY `unq_software_id_cve` (`software_id`,`cve`),
	// # Column types:
	// #	  `software_id` bigint unsigned default null
	// #	  `cve` varchar(255) collate utf8mb4_unicode_ci not null
	// # To remove this duplicate index, execute:
	// ALTER TABLE `fleet`.`software_cve` DROP INDEX `software_cve_software_id`;

	if indexExistsTx(tx, "app_config_json", "id") {
		if _, err := tx.Exec("ALTER TABLE `app_config_json` DROP INDEX `id`"); err != nil {
			return fmt.Errorf("failed to drop duplicate index id on app_config_json: %w", err)
		}
	}

	if indexExistsTx(tx, "host_users", "idx_uid_username") {
		if _, err := tx.Exec("ALTER TABLE `host_users` DROP INDEX `idx_uid_username`"); err != nil {
			return fmt.Errorf("failed to drop duplicate index idx_uid_username on host_users: %w", err)
		}
	}

	if indexExistsTx(tx, "migration_status_tables", "id") {
		if _, err := tx.Exec("ALTER TABLE `migration_status_tables` DROP INDEX `id`"); err != nil {
			return fmt.Errorf("failed to drop duplicate index id on migration_status_tables: %w", err)
		}
	}

	if indexExistsTx(tx, "policy_membership", "idx_policy_membership_policy_id") {
		if _, err := tx.Exec("ALTER TABLE `policy_membership` DROP INDEX `idx_policy_membership_policy_id`"); err != nil {
			return fmt.Errorf("failed to drop duplicate index idx_policy_membership_policy_id on policy_membership: %w", err)
		}
	}

	if indexExistsTx(tx, "software", "software_listing_idx") {
		if _, err := tx.Exec("ALTER TABLE `software` DROP INDEX `software_listing_idx`, ADD INDEX `software_listing_idx` (`name`)"); err != nil {
			return fmt.Errorf("failed to replace software_listing_idx on software: %w", err)
		}
	}

	if indexExistsTx(tx, "software_cve", "software_cve_software_id") {
		if _, err := tx.Exec("ALTER TABLE `software_cve` DROP INDEX `software_cve_software_id`"); err != nil {
			return fmt.Errorf("failed to drop duplicate index software_cve_software_id on software_cve: %w", err)
		}
	}

	return nil
}

func Down_20241122171434(tx *sql.Tx) error {
	return nil
}
