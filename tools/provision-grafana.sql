-- Configuration, not application schema. Password arrives only through the environment.
\getenv grafana_password GRAFANA_DB_PASSWORD
\getenv database POSTGRES_DB
SELECT 'CREATE ROLE buildlens_grafana' WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname='buildlens_grafana') \gexec
ALTER ROLE buildlens_grafana WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 5 PASSWORD :'grafana_password';
ALTER ROLE buildlens_grafana SET default_transaction_read_only=on;
ALTER ROLE buildlens_grafana SET statement_timeout='5s';
GRANT CONNECT ON DATABASE :"database" TO buildlens_grafana;
GRANT USAGE ON SCHEMA public TO buildlens_grafana;
GRANT SELECT ON repository,build,stage_run,test_case,test_run,quarantine,alert TO buildlens_grafana;
