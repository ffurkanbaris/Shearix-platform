#!/bin/sh
set -eu

setup_database() {
  database="$1" owner="$2" migrator="$3" application="$4" migrator_password="$5" application_password="$6"
  psql -v ON_ERROR_STOP=1 -v migrator_password="$migrator_password" -v application_password="$application_password" <<SQL
CREATE ROLE $owner NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
CREATE ROLE $migrator LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOBYPASSRLS PASSWORD :'migrator_password';
CREATE ROLE $application LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD :'application_password';
GRANT $owner TO $migrator;
CREATE DATABASE $database OWNER $owner;
REVOKE ALL ON DATABASE $database FROM PUBLIC;
GRANT CONNECT ON DATABASE $database TO $application;
SQL
  psql -v ON_ERROR_STOP=1 -d "$database" <<SQL
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
SQL
}

setup_database tenant_db tenant_db_owner tenant_db_migrator tenant_db_app "$TENANT_DB_OWNER_PASSWORD" "$TENANT_DB_APP_PASSWORD"
setup_database auth_db auth_db_owner auth_db_migrator auth_db_app "$AUTH_DB_OWNER_PASSWORD" "$AUTH_DB_APP_PASSWORD"
setup_database barber_db barber_db_owner barber_db_migrator barber_db_app "$BARBER_DB_OWNER_PASSWORD" "$BARBER_DB_APP_PASSWORD"
setup_database catalog_db catalog_db_owner catalog_db_migrator catalog_db_app "$CATALOG_DB_OWNER_PASSWORD" "$CATALOG_DB_APP_PASSWORD"
setup_database scheduling_db scheduling_db_owner scheduling_db_migrator scheduling_db_app "$SCHEDULING_DB_OWNER_PASSWORD" "$SCHEDULING_DB_APP_PASSWORD"
setup_database appointment_db appointment_db_owner appointment_db_migrator appointment_db_app "$APPOINTMENT_DB_OWNER_PASSWORD" "$APPOINTMENT_DB_APP_PASSWORD"
setup_database notification_db notification_db_owner notification_db_migrator notification_db_app "$NOTIFICATION_DB_OWNER_PASSWORD" "$NOTIFICATION_DB_APP_PASSWORD"
setup_database customer_db customer_db_owner customer_db_migrator customer_db_app "$CUSTOMER_DB_OWNER_PASSWORD" "$CUSTOMER_DB_APP_PASSWORD"
