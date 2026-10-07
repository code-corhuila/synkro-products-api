DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'products_writer') THEN
    CREATE ROLE products_writer NOLOGIN;
  END IF;
END $$;
