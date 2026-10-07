GRANT USAGE ON SCHEMA products_schema TO products_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA products_schema
  GRANT SELECT, INSERT, UPDATE ON TABLES TO products_writer;
GRANT products_writer TO products_app;
