-- Ajilamu provenance ledger, ClickHouse DDL.
--
-- Run this file into the database named by CLICKHOUSE_DATABASE. No statement names a
-- database, so the same file loads into a scratch database during tests. Load it with
-- clickhouse client --queries-file, because the HTTP endpoint refuses a multi statement
-- body.
--
-- Every CREATE TABLE uses IF NOT EXISTS and every view uses OR REPLACE. Running the file
-- twice in a row succeeds, keeps every row, and leaves the view definitions current. A run
-- that died halfway also recovers, because the next run creates whatever is missing.
--
-- IF NOT EXISTS alone is not safe, because it skips a table that already exists in some
-- other shape. The preflight guard below refuses to run against such a database. See the
-- comment above the guard.
--
-- Names. The five append targets carry the _raw suffix. The five plain names are views that
-- apply FINAL, so the obvious read is the correct read. SELECT sum(cost_usd) FROM charges
-- never counts a re-sent batch twice. A writer names charges_raw. ClickHouse refuses an
-- INSERT into a view, so a writer that names the plain table fails loudly.
--
-- The ledger is append-only. Nothing here updates or deletes a row. A correction arrives
-- as a new commit that points at its parent, exactly as a version control system records
-- a revert.
--
-- Every table runs ReplacingMergeTree and dedups on the natural identity of the event, not
-- on an id the client made up at send time. takes_raw, actions_raw and charges_raw each
-- carry a MATERIALIZED event_key that hashes the fields identifying the event itself, and
-- each sorting key closes on that column. The write queue in internal/ledger flushes and
-- reconciles after a network drop, so it re-sends batches it never saw acknowledged. A
-- re-sent row hashes to the same event_key and collapses back into one row, whatever id
-- the client generated for it, random UUID included.
--
-- take_id, charge_id and action_id stay on the row as the delivery id the client chose.
-- None of them decides dedup. A reconnect may regenerate a take id, and a fresh id must
-- not turn a re-sent batch into a second event. commits_raw keys on (dub_id, commit_id)
-- instead, because commit_id is the commit's own identity rather than a delivery id.
-- Children reference it through parent_commit_id, so a resend repeats the same value.
--
-- The natural identity of a charge covers the take, the attempt, the kind, the provider,
-- the unit, the count and the price. Two calls differing in any of those stay two rows. A
-- genuine second call that repeats every one of them must raise attempt, otherwise the
-- ledger reads it as one call delivered twice.
--
-- That collapse does not weaken append-only. Append-only forbids overwriting a different
-- logical event. Two rows sharing a natural identity are one event delivered twice, and
-- keeping both would inflate every count and every cost total.
--
-- ReplacingMergeTree collapses at merge time, so a fresh duplicate stays visible in the
-- _raw table until a merge runs. The plain named views apply FINAL and collapse it at read
-- time. Counting or summing a _raw table directly counts a retry twice.
--
-- Every table stamps ingested_at with the server clock and partitions by its month.
-- created_at stays a plain DEFAULT, because T1.3 names DEFAULT now64(3), so a writer may
-- supply its own value there. Partitions therefore stay few even when a writer clock
-- reads 2099. Recency filters in T4.7 name ingested_at, which prunes partitions. Both
-- columns are DateTime64(3). ingested_at is MATERIALIZED, so SELECT * skips it and the
-- views name it.

-- Preflight guard. This runs first and creates nothing.
--
-- The production service already carries a takes table from the pre-T1.3 validation run.
-- It holds take_id UUID, overrun_pct Float32 and cost_usd Float64, and it carries no
-- commit_id, no delta_ms and no peaks. CREATE TABLE IF NOT EXISTS skips a table like that
-- in silence, so the file would exit 0 and leave every writer pointed at the wrong shape.
--
-- The guard compares every property this file owns. Each _raw table holds exactly the
-- columns listed below at the listed types, and no column this file never declares. Each
-- _raw table runs a replacing engine with exactly the argument list listed below. Each
-- _raw table carries the sorting key, the partition key and the default or MATERIALIZED
-- expression listed below for every column. Each table carries exactly the constraints
-- listed below, no more and no fewer. Each plain name is either free or already holds a
-- view.
--
-- Any one of those failing aborts the run before the first CREATE. An empty database
-- matches nothing and passes. A database this file already created matches everything and
-- passes, so a second run and a resumed half run both pass.
--
-- The old takes cost_usd rule now falls out of the column list. takes_raw declares no
-- cost_usd, so a table carrying one fails the undeclared column check.
--
-- Guard text must match what each server reports for its own build of this file.
-- Comparisons therefore strip whitespace from expressions. Cloud writes shared macro
-- arguments into the engine text, and the engine branch removes them before comparing.
--
WITH
    expected_columns AS
    (
        SELECT arrayJoin([
        ('actions_raw', 'action_id', 'String'),
        ('actions_raw', 'commit_id', 'String'),
        ('actions_raw', 'project_id', 'String'),
        ('actions_raw', 'dub_id', 'String'),
        ('actions_raw', 'owner_id', 'String'),
        ('actions_raw', 'language', 'LowCardinality(String)'),
        ('actions_raw', 'segment_index', 'Int32'),
        ('actions_raw', 'take_id', 'String'),
        ('actions_raw', 'action_type', 'LowCardinality(String)'),
        ('actions_raw', 'author', 'Enum8(\'agent\' = 1, \'command_bar\' = 2, \'manual_ui\' = 3)'),
        ('actions_raw', 'prompt', 'String'),
        ('actions_raw', 'before_value', 'String'),
        ('actions_raw', 'after_value', 'String'),
        ('actions_raw', 'created_at', 'DateTime64(3)'),
        ('actions_raw', 'ingested_at', 'DateTime64(3)'),
        ('actions_raw', 'event_key', 'String'),
        ('charges_raw', 'charge_id', 'String'),
        ('charges_raw', 'take_id', 'String'),
        ('charges_raw', 'commit_id', 'String'),
        ('charges_raw', 'project_id', 'String'),
        ('charges_raw', 'dub_id', 'String'),
        ('charges_raw', 'owner_id', 'String'),
        ('charges_raw', 'language', 'LowCardinality(String)'),
        ('charges_raw', 'segment_index', 'Int32'),
        ('charges_raw', 'attempt', 'UInt8'),
        ('charges_raw', 'kind', 'Enum8(\'segment\' = 1, \'translate\' = 2, \'synthesize\' = 3)'),
        ('charges_raw', 'provider', 'LowCardinality(String)'),
        ('charges_raw', 'unit', 'LowCardinality(String)'),
        ('charges_raw', 'units', 'Decimal(18, 4)'),
        ('charges_raw', 'unit_price_usd', 'Decimal(18, 10)'),
        ('charges_raw', 'cost_usd', 'Decimal(38, 14)'),
        ('charges_raw', 'created_at', 'DateTime64(3)'),
        ('charges_raw', 'ingested_at', 'DateTime64(3)'),
        ('charges_raw', 'event_key', 'String'),
        ('commits_raw', 'commit_id', 'String'),
        ('commits_raw', 'parent_commit_id', 'String'),
        ('commits_raw', 'project_id', 'String'),
        ('commits_raw', 'dub_id', 'String'),
        ('commits_raw', 'owner_id', 'String'),
        ('commits_raw', 'branch', 'LowCardinality(String)'),
        ('commits_raw', 'language', 'LowCardinality(String)'),
        ('commits_raw', 'version_seq', 'UInt64'),
        ('commits_raw', 'message', 'String'),
        ('commits_raw', 'created_at', 'DateTime64(3)'),
        ('commits_raw', 'ingested_at', 'DateTime64(3)'),
        ('takes_raw', 'take_id', 'String'),
        ('takes_raw', 'commit_id', 'String'),
        ('takes_raw', 'project_id', 'String'),
        ('takes_raw', 'dub_id', 'String'),
        ('takes_raw', 'owner_id', 'String'),
        ('takes_raw', 'language', 'LowCardinality(String)'),
        ('takes_raw', 'segment_index', 'Int32'),
        ('takes_raw', 'attempt', 'UInt8'),
        ('takes_raw', 'speaker', 'LowCardinality(String)'),
        ('takes_raw', 'voice', 'LowCardinality(String)'),
        ('takes_raw', 'text', 'String'),
        ('takes_raw', 'slot_start_ms', 'Int64'),
        ('takes_raw', 'slot_ms', 'Int32'),
        ('takes_raw', 'measured_ms', 'Int32'),
        ('takes_raw', 'delta_ms', 'Int32'),
        ('takes_raw', 'repair', 'Enum8(\'none\' = 1, \'atempo\' = 2, \'rewrite\' = 3, \'manual\' = 4)'),
        ('takes_raw', 'repair_detail', 'String'),
        ('takes_raw', 'audio_path', 'String'),
        ('takes_raw', 'peaks', 'Array(UInt8)'),
        ('takes_raw', 'created_at', 'DateTime64(3)'),
        ('takes_raw', 'ingested_at', 'DateTime64(3)'),
        ('takes_raw', 'event_key', 'String'),
        ('timeline_state_raw', 'commit_id', 'String'),
        ('timeline_state_raw', 'project_id', 'String'),
        ('timeline_state_raw', 'dub_id', 'String'),
        ('timeline_state_raw', 'owner_id', 'String'),
        ('timeline_state_raw', 'language', 'LowCardinality(String)'),
        ('timeline_state_raw', 'version_seq', 'UInt64'),
        ('timeline_state_raw', 'segment_index', 'Int32'),
        ('timeline_state_raw', 'start_ms', 'Int64'),
        ('timeline_state_raw', 'end_ms', 'Int64'),
        ('timeline_state_raw', 'speaker', 'LowCardinality(String)'),
        ('timeline_state_raw', 'emotion', 'LowCardinality(String)'),
        ('timeline_state_raw', 'source_text', 'String'),
        ('timeline_state_raw', 'text', 'String'),
        ('timeline_state_raw', 'take_id', 'String'),
        ('timeline_state_raw', 'created_at', 'DateTime64(3)'),
        ('timeline_state_raw', 'ingested_at', 'DateTime64(3)')
        ]) AS c
    ),
    expected_keys AS
    (
        SELECT arrayJoin([
            ('takes_raw', 'dub_id, language, commit_id, segment_index, attempt, event_key'),
            ('commits_raw', 'dub_id, commit_id'),
            ('timeline_state_raw', 'dub_id, language, commit_id, segment_index'),
            ('actions_raw', 'dub_id, commit_id, event_key'),
            ('charges_raw', 'dub_id, language, commit_id, segment_index, attempt, kind, event_key')
        ]) AS k
    ),
    expected_defaults AS
    (
        -- Every column that carries a default or a MATERIALIZED expression, with the
        -- expression exactly as the server reports it. Columns absent from this list
        -- must carry no default at all.
        SELECT arrayJoin([
            ('actions_raw', 'action_id', 'DEFAULT', '\'\''),
            ('actions_raw', 'after_value', 'DEFAULT', '\'\''),
            ('actions_raw', 'before_value', 'DEFAULT', '\'\''),
            ('actions_raw', 'created_at', 'DEFAULT', 'now64(3)'),
            ('actions_raw', 'event_key', 'MATERIALIZED', 'lower(hex(sipHash128(dub_id, toString(language), commit_id, toString(segment_index), toString(action_type), toString(author), take_id, prompt, before_value, after_value)))'),
            ('actions_raw', 'ingested_at', 'MATERIALIZED', 'now64(3)'),
            ('actions_raw', 'language', 'DEFAULT', '\'\''),
            ('actions_raw', 'prompt', 'DEFAULT', '\'\''),
            ('actions_raw', 'segment_index', 'DEFAULT', '-1'),
            ('actions_raw', 'take_id', 'DEFAULT', '\'\''),
            ('charges_raw', 'attempt', 'DEFAULT', '0'),
            ('charges_raw', 'charge_id', 'DEFAULT', '\'\''),
            ('charges_raw', 'commit_id', 'DEFAULT', '\'\''),
            ('charges_raw', 'cost_usd', 'MATERIALIZED', 'toDecimal128(units, 4) * unit_price_usd'),
            ('charges_raw', 'created_at', 'DEFAULT', 'now64(3)'),
            ('charges_raw', 'event_key', 'MATERIALIZED', 'lower(hex(sipHash128(dub_id, toString(language), commit_id, toString(segment_index), toString(attempt), toString(kind), toString(provider), toString(unit), toString(units), toString(unit_price_usd))))'),
            ('charges_raw', 'ingested_at', 'MATERIALIZED', 'now64(3)'),
            ('charges_raw', 'language', 'DEFAULT', '\'\''),
            ('charges_raw', 'provider', 'DEFAULT', '\'\''),
            ('charges_raw', 'segment_index', 'DEFAULT', '-1'),
            ('charges_raw', 'take_id', 'DEFAULT', '\'\''),
            ('charges_raw', 'unit', 'DEFAULT', '\'\''),
            ('commits_raw', 'branch', 'DEFAULT', '\'main\''),
            ('commits_raw', 'created_at', 'DEFAULT', 'now64(3)'),
            ('commits_raw', 'ingested_at', 'MATERIALIZED', 'now64(3)'),
            ('commits_raw', 'language', 'DEFAULT', '\'\''),
            ('commits_raw', 'message', 'DEFAULT', '\'\''),
            ('commits_raw', 'parent_commit_id', 'DEFAULT', '\'\''),
            ('takes_raw', 'created_at', 'DEFAULT', 'now64(3)'),
            ('takes_raw', 'event_key', 'MATERIALIZED', 'lower(hex(sipHash128(dub_id, toString(language), commit_id, toString(segment_index), toString(attempt))))'),
            ('takes_raw', 'ingested_at', 'MATERIALIZED', 'now64(3)'),
            ('takes_raw', 'peaks', 'DEFAULT', '[]'),
            ('takes_raw', 'repair_detail', 'DEFAULT', '\'\''),
            ('timeline_state_raw', 'created_at', 'DEFAULT', 'now64(3)'),
            ('timeline_state_raw', 'emotion', 'DEFAULT', '\'\''),
            ('timeline_state_raw', 'ingested_at', 'MATERIALIZED', 'now64(3)'),
            ('timeline_state_raw', 'language', 'DEFAULT', '\'\''),
            ('timeline_state_raw', 'source_text', 'DEFAULT', '\'\''),
            ('timeline_state_raw', 'speaker', 'DEFAULT', '\'\''),
            ('timeline_state_raw', 'take_id', 'DEFAULT', '\'\''),
            ('timeline_state_raw', 'text', 'DEFAULT', '\'\'')
        ]) AS d
    ),
    expected_partitions AS
    (
        SELECT arrayJoin([
            ('actions_raw', 'toYYYYMM(ingested_at)'),
            ('charges_raw', 'toYYYYMM(ingested_at)'),
            ('commits_raw', 'toYYYYMM(ingested_at)'),
            ('takes_raw', 'toYYYYMM(ingested_at)'),
            ('timeline_state_raw', 'toYYYYMM(ingested_at)')
        ]) AS p
    ),
    expected_engines AS
    (
        SELECT arrayJoin([
            ('actions_raw', 'ReplacingMergeTree'),
            ('charges_raw', 'ReplacingMergeTree'),
            ('commits_raw', 'ReplacingMergeTree'),
            ('takes_raw', 'ReplacingMergeTree'),
            ('timeline_state_raw', 'ReplacingMergeTree(ingested_at)')
        ]) AS e
    ),
    expected_constraints AS
    (
        SELECT arrayJoin([
            ('charges_raw', 'CONSTRAINT units_are_positive CHECK units >= 0'),
            ('charges_raw', 'CONSTRAINT price_is_positive CHECK unit_price_usd >= 0'),
            ('takes_raw', 'CONSTRAINT delta_is_signed CHECK delta_ms = (measured_ms - slot_ms)'),
            ('takes_raw', 'CONSTRAINT peaks_are_sized CHECK (length(peaks) = 0) OR ((length(peaks) >= 64) AND (length(peaks) <= 128))'),
            ('takes_raw', 'CONSTRAINT segment_index_is_real CHECK segment_index >= 0'),
            ('timeline_state_raw', 'CONSTRAINT slot_is_positive CHECK end_ms > start_ms'),
            ('timeline_state_raw', 'CONSTRAINT segment_index_is_real CHECK segment_index >= 0')
        ]) AS x
    ),
    full_defaults AS
    (
        -- The declared column list joined to the default map, so every column carries
        -- an expected kind and expression. Columns absent from the map expect none.
        SELECT
            c.1 AS object,
            c.2 AS column_name,
            if(d.1 IS NULL, '', d.3) AS default_kind,
            if(d.1 IS NULL, '', d.4) AS default_expression
        FROM expected_columns AS c
        LEFT JOIN expected_defaults AS d ON d.1 = c.1 AND d.2 = c.2
    ),
    server_columns AS
    (
        -- What the server actually reports for every column of the current database.
        SELECT
            table AS object,
            name AS column_name,
            default_kind,
            replaceAll(default_expression, ' ', '') AS canonical_expression
        FROM system.columns
        WHERE database = currentDatabase()
    ),
    engine_tokens AS
    (
        -- The user argument list of each replacing engine, with Cloud's shared macro
        -- arguments removed. Cloud writes two macro arguments after the engine name,
        -- for example SharedReplacingMergeTree('<path>', '<replica>', ingested_at).
        -- The macro arguments carry no comma, so the user arguments follow the second
        -- comma. Local writes no macro block, so the whole parenthesized tail counts.
        SELECT
            name,
            if(
                position(token, '(') = 0,
                '',
                if(
                    startsWith(token, 'SharedReplacingMergeTree('),
                    arrayStringConcat(
                        arraySlice(
                            splitByChar(
                                ',',
                                substring(
                                    token,
                                    length('SharedReplacingMergeTree(') + 1,
                                    length(token) - length('SharedReplacingMergeTree(') - 1
                                )
                            ),
                            3
                        ),
                        ','
                    ),
                    substring(
                        token,
                        position(token, '(') + 1,
                        length(token) - position(token, '(') - 1
                    )
                )
            ) AS user_args
        FROM
        (
            SELECT
                name,
                replaceAll(
                    substring(engine_full, 1, position(engine_full, 'PARTITION BY') - 1),
                    ' ', ''
                ) AS token
            FROM system.tables
            WHERE database = currentDatabase()
              AND name IN (SELECT DISTINCT e.1 FROM expected_engines)
              AND engine IN ('ReplacingMergeTree', 'SharedReplacingMergeTree')
        )
    )
SELECT throwIf(
    count() > 0,
    'Ajilamu schema.sql preflight failed. This database already holds a ledger object in an incompatible shape, so the file created nothing. A column is missing, a type is wrong, a column is undeclared, an engine or its parameters are wrong, a sorting key, a partition key, a materialized expression or a constraint is wrong, or a read view name is held by a table. Point CLICKHOUSE_DATABASE at an empty database, or drop or rename the existing object, then run the file again. To see the mismatch, select table, name, default_kind, default_expression and type from system.columns for the current database, and select name, engine, engine_full, sorting_key, partition_key and create_table_query from system.tables.'
) AS preflight_ok
FROM
(
    -- A table this file owns exists without a column it declares, or with a wrong type.
    SELECT c.1 AS object, c.2 AS detail, 'missing column or wrong type' AS problem
    FROM expected_columns
    WHERE c.1 IN (SELECT name FROM system.tables WHERE database = currentDatabase())
      AND (c.1, c.2, c.3) NOT IN (
          SELECT (table, name, type) FROM system.columns WHERE database = currentDatabase()
      )

    UNION ALL

    -- A table this file owns carries a column this file never declares.
    SELECT table AS object, name AS detail, 'undeclared column' AS problem
    FROM system.columns
    WHERE database = currentDatabase()
      AND table IN (SELECT DISTINCT c.1 FROM expected_columns)
      AND (table, name) NOT IN (SELECT (c.1, c.2) FROM expected_columns)

    UNION ALL

    -- A table this file owns runs an engine that does not collapse a re-sent row.
    SELECT name AS object, engine AS detail, 'wrong engine' AS problem
    FROM system.tables
    WHERE database = currentDatabase()
      AND name IN (SELECT DISTINCT c.1 FROM expected_columns)
      AND engine NOT IN ('ReplacingMergeTree', 'SharedReplacingMergeTree')

    UNION ALL

    -- A table this file owns dedups on a different key than this file declares.
    SELECT k.1 AS object, k.2 AS detail, 'wrong sorting key' AS problem
    FROM expected_keys
    WHERE k.1 IN (SELECT name FROM system.tables WHERE database = currentDatabase())
      AND (k.1, replaceAll(k.2, ' ', '')) NOT IN (
          SELECT (name, replaceAll(sorting_key, ' ', ''))
          FROM system.tables WHERE database = currentDatabase()
      )

    UNION ALL

    -- A read view name is held by something that is not a view.
    SELECT name AS object, engine AS detail, 'read name held by a table' AS problem
    FROM system.tables
    WHERE database = currentDatabase()
      AND name IN (
          'takes', 'commits', 'timeline_state', 'actions', 'charges',
          'take_rates', 'timeline_at_commit'
      )
      AND engine != 'View'

    UNION ALL

    -- A declared column must carry the exact default or MATERIALIZED kind and the exact
    -- expression. The first arm checks the file's expectations against the server.
    SELECT f.object AS object, f.column_name AS detail, 'wrong column default' AS problem
    FROM full_defaults AS f
    WHERE f.object IN (SELECT name FROM system.tables WHERE database = currentDatabase())
      AND (f.object, f.column_name, f.default_kind,
           replaceAll(f.default_expression, ' ', '')) NOT IN (
          SELECT object, column_name, default_kind, canonical_expression
          FROM server_columns
      )

    UNION ALL

    -- The second arm checks the server's columns against the file's expectations, so a
    -- plain column turned MATERIALIZED or DEFAULT on the server aborts the run.
    SELECT s.object AS object, s.column_name AS detail, 'undeclared column default' AS problem
    FROM server_columns AS s
    WHERE s.object IN (SELECT DISTINCT c.1 FROM expected_columns)
      AND (s.object, s.column_name, s.default_kind, s.canonical_expression) NOT IN (
          SELECT object, column_name, default_kind,
                 replaceAll(default_expression, ' ', '')
          FROM full_defaults
      )

    UNION ALL

    -- A table this file owns buckets rows by a different month than this file declares.
    SELECT p.1 AS object, p.2 AS detail, 'wrong partition key' AS problem
    FROM expected_partitions AS p
    WHERE p.1 IN (SELECT name FROM system.tables WHERE database = currentDatabase())
      AND (p.1, replaceAll(p.2, ' ', '')) NOT IN (
          SELECT name, replaceAll(partition_key, ' ', '')
          FROM system.tables WHERE database = currentDatabase()
      )

    UNION ALL

    -- A table this file owns must carry exactly the engine argument list listed below.
    -- timeline_state_raw keeps the version column ingested_at. The other four tables
    -- carry no argument, because a column in that slot acts as a version or deletion
    -- flag under FINAL. Cloud's macro arguments are already removed in engine_tokens.
    SELECT e.1 AS object, e.2 AS detail, 'wrong engine parameters' AS problem
    FROM expected_engines AS e
    INNER JOIN engine_tokens AS t ON t.name = e.1
    WHERE t.user_args != if(
        position(e.2, '(') = 0,
        '',
        substring(e.2, position(e.2, '(') + 1, length(e.2) - position(e.2, '(') - 1)
    )

    UNION ALL

    -- A table this file owns misses a constraint or checks a different expression.
    -- The exact fragment must sit in the server DDL with nothing appended after it,
    -- so a check weakened by extra OR terms still aborts the run.
    SELECT x.1 AS object, x.2 AS detail, 'wrong constraint' AS problem
    FROM expected_constraints AS x
    INNER JOIN (
        SELECT name, create_table_query FROM system.tables WHERE database = currentDatabase()
    ) AS t ON t.name = x.1
    WHERE if(
        position(replaceAll(t.create_table_query, ' ', ''), replaceAll(x.2, ' ', '')) = 0,
        1,
        not(
            substring(
                replaceAll(t.create_table_query, ' ', ''),
                position(replaceAll(t.create_table_query, ' ', ''), replaceAll(x.2, ' ', ''))
                    + length(replaceAll(x.2, ' ', '')),
                1
            ) IN (',', ')')
        )
    )

    UNION ALL

    -- A table this file owns carries a constraint this file never declares. Each
    -- CONSTRAINT token in the server DDL adds ten characters, so the length difference
    -- counts them exactly.
    SELECT t.name AS object, 'unexpected CONSTRAINT in DDL' AS detail,
           'undeclared constraint' AS problem
    FROM system.tables AS t
    LEFT JOIN (
        SELECT x.1 AS table_name, count() AS n
        FROM expected_constraints AS x
        GROUP BY x.1
    ) AS cc ON cc.table_name = t.name
    WHERE t.database = currentDatabase()
      AND t.name IN (SELECT DISTINCT c.1 FROM expected_columns)
      AND (length(t.create_table_query) - length(replaceAll(t.create_table_query, 'CONSTRAINT', '')))
          != if(cc.n IS NULL, 0, cc.n) * length('CONSTRAINT')
);

-- takes_raw: one row per take attempt, including rejected attempts and repaired renders.
--
-- The row stores a signed delta in milliseconds. It stores no percentage, because a
-- percentage hides direction once someone takes its absolute value. Segment 8 of the
-- validation run reads -2910 here, and any reader sees the underrun.
--
-- The row carries no cost column. A take costs the sum of its own rows in charges. That
-- rule kills the double counting that the first run produced when it accumulated cost
-- across attempts.
--
-- ORDER BY (dub_id, language, commit_id, segment_index, attempt, event_key). The five
-- leading fields name the natural identity of a render, the commit included. charges_raw
-- repeats that prefix, so the take panel join and the branch cost rollup read one
-- contiguous run per take and never double across two branches.
--
-- event_key hashes the natural identity of a render, which is the dub, the language, the
-- commit, the segment and the attempt. take_id stays on the row and never decides dedup, so
-- two commits that reuse one take_id keep two rows.
--
-- T4.7 reads take_rates at the end of this file rather than this table. That view already
-- applies FINAL, so its count() is the deduplicated sample size T4.7 must report.
CREATE TABLE IF NOT EXISTS takes_raw
(
    -- Identity. charges rows point at take_id.
    take_id       String,
    -- The commit that produced this take. Time travel selects takes by commit set.
    commit_id     String,
    project_id    String,
    dub_id        String,
    -- The creator who owns the dub. T4.7 weighs personal history against the population.
    owner_id      String,
    language      LowCardinality(String),
    -- Int32 across takes, actions, charges and timeline_state, so one Go field type
    -- covers every table. A take always names a real segment, which the constraint below
    -- enforces. Only actions and charges use the -1 sentinel for a dub wide row.
    segment_index Int32,
    attempt       UInt8,
    speaker       LowCardinality(String),
    voice         LowCardinality(String),
    -- The line this take speaks, after translation. Learned priors divide its length by
    -- measured_ms to get a speech rate.
    text          String,
    -- Where the slot opens on the source timeline.
    slot_start_ms Int64,
    -- Target slot length and the measured length of the rendered file.
    slot_ms       Int32,
    measured_ms   Int32,
    -- Signed fit delta. Positive runs long, negative runs short.
    delta_ms      Int32,
    -- Repair strategy. The names match types.Repair.String() exactly. An unknown strategy
    -- fails the insert rather than rendering as an unknown state on the length bar.
    repair        Enum8('none' = 1, 'atempo' = 2, 'rewrite' = 3, 'manual' = 4),
    -- Free text detail for the repair, such as "atempo 1.075 -> 5338".
    repair_detail String DEFAULT '',
    -- Content addressed path to the discrete WAV take on disk.
    audio_path    String,
    -- Normalized waveform peaks, 64 to 128 per take. The timeline draws from these and
    -- never opens the audio file.
    peaks         Array(UInt8) DEFAULT [],
    created_at    DateTime64(3) DEFAULT now64(3),
    -- Server clock, and the partition key. See the header.
    ingested_at   DateTime64(3) MATERIALIZED now64(3),
    -- The natural identity of this render, hashed. The server computes it, so a client
    -- cannot defeat dedup by inventing a new id.
    event_key     String MATERIALIZED lower(hex(sipHash128(dub_id, toString(language), commit_id, toString(segment_index), toString(attempt)))),

    -- The delta stays signed and stays consistent. An insert that computes a one-sided or
    -- absolute value fails here instead of reaching a reader.
    CONSTRAINT delta_is_signed CHECK delta_ms = measured_ms - slot_ms,
    -- Peaks arrive empty until T3.5 renders them, or they arrive within the stated range.
    CONSTRAINT peaks_are_sized CHECK length(peaks) = 0 OR (length(peaks) >= 64 AND length(peaks) <= 128),
    -- Int32 admits -1, and a take never carries the dub wide sentinel.
    CONSTRAINT segment_index_is_real CHECK segment_index >= 0
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(ingested_at)
ORDER BY (dub_id, language, commit_id, segment_index, attempt, event_key);

-- commits_raw: the immutable edit DAG.
--
-- A root commit carries an empty parent_commit_id. A branch starts by writing a new commit
-- whose parent points at any earlier commit, which forks the chain without touching it.
--
-- Time travel walks parent_commit_id upward from the target commit. A recursive CTE does
-- the walk, seeded with the target commit and joined back to commits on
-- commits.commit_id = ancestry.parent_commit_id. That set is the exact ancestry. The
-- timeline_at_commit view at the end of this file runs exactly that walk.
--
-- Filtering on version_seq alone is wrong once a branch exists. version_seq counts per dub
-- and branch, so two branches reuse a number, and a range filter pulls in commits that are
-- not ancestors. Filtering on branch alone is also wrong, because it drops the shared
-- history the fork inherited from main.
--
-- ORDER BY (dub_id, commit_id) because the walk looks a commit up by id at every step. That
-- pair is the natural identity of a commit, so it also collapses a resend. version_seq
-- still orders the history panel, which reads one dub and sorts a small result.
CREATE TABLE IF NOT EXISTS commits_raw
(
    commit_id        String,
    -- Empty for the root commit of a dub.
    parent_commit_id String DEFAULT '',
    project_id       String,
    dub_id           String,
    owner_id         String,
    -- Branch label. A fork names its own branch and leaves main untouched.
    branch           LowCardinality(String) DEFAULT 'main',
    -- The language track this commit changes. Empty means it changes the whole dub.
    language         LowCardinality(String) DEFAULT '',
    -- Monotonic per dub and branch, and strictly increasing along any ancestry path. That
    -- makes it the ordering key for argMax over an ancestor set. It never selects that set.
    version_seq      UInt64,
    -- One sentence describing the commit, rendered directly in the history panel.
    message          String DEFAULT '',
    -- When the writer says the commit happened. T1.3 names DEFAULT now64(3), so a writer
    -- may override it and this column never decides commit order. parent_commit_id and
    -- version_seq decide order, and the history panel sorts on version_seq.
    created_at       DateTime64(3) DEFAULT now64(3),
    -- Server clock, and the partition key. See the header.
    ingested_at      DateTime64(3) MATERIALIZED now64(3)
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(ingested_at)
ORDER BY (dub_id, commit_id);

-- timeline_state_raw: the timeline as each commit left it, one row per segment the commit
-- changed.
--
-- T4.5 requires that state at commit N reads the same whether a reader replays the log or
-- queries it. Replaying JSON out of actions is not a query, and an edit that renders no
-- take leaves nothing on takes to read. So each commit writes its changed segments here.
--
-- A row is a full snapshot of one segment, never a delta. The writer copies every field
-- forward, including the fields this commit did not touch. A boundary nudge therefore
-- restates the take, the speaker and both texts at their current values. That rule is what
-- makes the read correct, because the read takes the newest ancestor row per segment and
-- never merges two rows together.
--
-- One commit leaves at most one row per segment. The sorting key is the natural identity,
-- and the engine keeps the row with the highest ingested_at. Two edits to one segment
-- inside one commit therefore collapse to the state that commit ended with, which is the
-- answer a reader wants.
--
-- version_seq repeats the value on the commit row. It stays derivable from commit_id, and
-- carrying it here lets the state query rank rows without a join.
--
-- Read state through timeline_at_commit at the end of this file. It walks the ancestry and
-- runs argMax on version_seq per segment. Do not filter on version_seq alone, and do not
-- filter on branch alone. See the commits_raw comment.
--
-- ORDER BY (dub_id, language, commit_id, segment_index) because the state query filters an
-- ancestor set of commit ids. That key is the natural identity of a snapshot, so it also
-- collapses a resend.
CREATE TABLE IF NOT EXISTS timeline_state_raw
(
    commit_id     String,
    project_id    String,
    dub_id        String,
    owner_id      String,
    language      LowCardinality(String) DEFAULT '',
    version_seq   UInt64,
    segment_index Int32,
    -- Segment boundaries on the source timeline, as this commit left them.
    start_ms      Int64,
    end_ms        Int64,
    speaker       LowCardinality(String) DEFAULT '',
    emotion       LowCardinality(String) DEFAULT '',
    -- The original line, before translation. Nothing else in the ledger stores it, so a
    -- text correction on the source has somewhere to land.
    source_text   String DEFAULT '',
    -- The line as the dub speaks it, after translation and any correction.
    text          String DEFAULT '',
    -- The take that renders this segment at this commit. Empty when no take exists yet,
    -- which is the normal state after a boundary nudge or a text correction.
    take_id       String DEFAULT '',
    created_at    DateTime64(3) DEFAULT now64(3),
    -- Server clock, the partition key, and the version this engine keeps. The later write
    -- for one commit and segment wins, so a second edit inside one commit survives.
    ingested_at   DateTime64(3) MATERIALIZED now64(3),

    -- A segment ends after it starts. An inverted or empty slot means a broken editor.
    CONSTRAINT slot_is_positive CHECK end_ms > start_ms,
    -- Timeline state always names a real segment.
    CONSTRAINT segment_index_is_real CHECK segment_index >= 0
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(ingested_at)
ORDER BY (dub_id, language, commit_id, segment_index);

-- actions_raw: what each commit did, who asked for it, and what changed.
--
-- before_value and after_value hold JSON text. A boundary nudge stores millisecond
-- boundaries. A text correction stores the two strings. Keeping both sides means a reader
-- explains an edit without replaying the pipeline.
--
-- These two columns explain an edit. They do not carry state. timeline_state carries the
-- state that a commit leaves behind, and a reader queries that rather than parsing JSON.
--
-- ORDER BY (dub_id, commit_id, event_key) because the history panel loads every action of
-- one commit, and audit queries scan one dub.
--
-- event_key hashes the natural identity of an action, which is the commit, the segment, the
-- type, the author, the prompt, the take and both values. Two different edits to one
-- segment inside one commit hash differently and both survive. action_id stays on the row
-- as the delivery id and never decides dedup.
CREATE TABLE IF NOT EXISTS actions_raw
(
    -- The id the client gave this delivery. Informational, and never the dedup key.
    action_id     String DEFAULT '',
    commit_id     String,
    project_id    String,
    dub_id        String,
    owner_id      String,
    language      LowCardinality(String) DEFAULT '',
    -- The segment this action touches. Dub wide actions store -1.
    segment_index Int32 DEFAULT -1,
    -- The take this action produced, when it produced one. Otherwise empty.
    take_id       String DEFAULT '',
    -- Action vocabulary. PHASE-4 T4.4 names segment_created, boundary_nudged,
    -- speaker_reassigned, text_corrected, take_rendered, stretched, line_rewritten and
    -- user_command. project.md names the same events as detect_segments, nudge_boundary,
    -- reassign_speaker, render_take, stretch_audio, rewrite_line and user_command. The two
    -- lists disagree, so this column stays a string and T4.4 picks the vocabulary.
    action_type   LowCardinality(String),
    -- Who authored the change. An unknown author fails the insert rather than landing
    -- unattributed.
    author        Enum8('agent' = 1, 'command_bar' = 2, 'manual_ui' = 3),
    -- The creator's own words, stored verbatim for command bar edits.
    prompt        String DEFAULT '',
    before_value  String DEFAULT '',
    after_value   String DEFAULT '',
    created_at    DateTime64(3) DEFAULT now64(3),
    -- Server clock, and the partition key. See the header.
    ingested_at   DateTime64(3) MATERIALIZED now64(3),
    -- The natural identity of this edit, hashed. The server computes it, so a client
    -- cannot defeat dedup by inventing a new id.
    event_key     String MATERIALIZED lower(hex(sipHash128(dub_id, toString(language), commit_id, toString(segment_index), toString(action_type), toString(author), take_id, prompt, before_value, after_value)))
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(ingested_at)
ORDER BY (dub_id, commit_id, event_key);

-- charges_raw: one row per billed API call.
--
-- The cost model writes units and unit price. cost_usd derives from them, so no writer can
-- store an accumulated running total in it. Summing charges for one take gives that take's
-- cost, and summing charges for one dub gives the dub's cost. Neither sum counts a call
-- twice.
--
-- Sum through the charges view, which applies FINAL. A reconnecting write queue re-sends a
-- batch it never saw acknowledged, and this table shows both copies until a merge runs.
--
-- A segmentation call runs once per dub before any take exists. Those rows carry an empty
-- take_id, a segment_index of -1 and an attempt of 0. They also name the commit of the
-- pass that billed them, so a whole pass keeps a stable identity of its own.
--
-- ORDER BY (dub_id, language, commit_id, segment_index, attempt, kind, event_key). The
-- five leading fields repeat the natural identity of the take that paid for the call.
-- takes_raw shares that prefix, so a branch cost rollup never merges two commits' rows.
-- kind splits the call types a cost rollup groups.
--
-- event_key hashes the natural identity of a billed call, which is the identity of the
-- take it paid for plus the call facts. The take identity covers the dub, the language,
-- the commit, the segment and the attempt. The call facts add the kind, the provider,
-- the unit, the count and the price. take_id and charge_id stay on the row as delivery
-- ids and never decide dedup, so a reconnect that regenerates either id still collapses.
CREATE TABLE IF NOT EXISTS charges_raw
(
    -- The id the client gave this delivery. Informational, and never the dedup key.
    charge_id      String DEFAULT '',
    -- The take these units paid for. Informational, like charge_id. Empty for a whole
    -- pass call, which the row's own identity fields still tell apart.
    take_id        String DEFAULT '',
    commit_id      String DEFAULT '',
    project_id     String,
    dub_id         String,
    owner_id       String,
    language       LowCardinality(String) DEFAULT '',
    -- Take identity repeated so a cost rollup needs no join.
    segment_index  Int32 DEFAULT -1,
    -- The attempt of the take that paid for this call. Whole pass rows carry 0.
    attempt        UInt8 DEFAULT 0,
    -- What we paid for. T1.2 fixes these three kinds.
    kind           Enum8('segment' = 1, 'translate' = 2, 'synthesize' = 3),
    -- The model or voice that billed us, such as gemini-3.8-flash or chirp-3-hd.
    provider       LowCardinality(String) DEFAULT '',
    -- What we counted, such as characters, input_tokens, output_tokens or seconds.
    unit           LowCardinality(String) DEFAULT '',
    units          Decimal(18, 4),
    unit_price_usd Decimal(18, 10),
    -- Derived, never written. SELECT * skips it, so the charges view names it.
    cost_usd       Decimal128(14) MATERIALIZED toDecimal128(units, 4) * unit_price_usd,
    created_at     DateTime64(3) DEFAULT now64(3),
    -- Server clock, and the partition key. See the header.
    ingested_at    DateTime64(3) MATERIALIZED now64(3),
    -- The natural identity of this billed call, hashed. The take it paid for and the
    -- call facts decide the key. A regenerated take id never changes it, and the server
    -- computes it, so a client cannot defeat dedup by inventing a new id.
    event_key      String MATERIALIZED lower(hex(sipHash128(dub_id, toString(language), commit_id, toString(segment_index), toString(attempt), toString(kind), toString(provider), toString(unit), toString(units), toString(unit_price_usd)))),

    -- A negative count or a negative price means a broken caller, not a refund.
    CONSTRAINT units_are_positive CHECK units >= 0,
    CONSTRAINT price_is_positive CHECK unit_price_usd >= 0
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(ingested_at)
ORDER BY (dub_id, language, commit_id, segment_index, attempt, kind, event_key);

-- Read views. These carry the plain names, so the obvious read is the safe read.
--
-- Each view applies FINAL, so it collapses a re-sent batch immediately instead of waiting
-- for a merge. Counts, cost sums and prior sample sizes stay steady across a retry.
--
-- Each view also names the MATERIALIZED columns, which SELECT * on a base table skips.
--
-- A writer names the _raw table. ClickHouse refuses an INSERT into these views, so a writer
-- that names the plain table gets an error rather than a row outside the dedup path.
CREATE OR REPLACE VIEW takes AS
    SELECT *, event_key, ingested_at FROM takes_raw FINAL;

CREATE OR REPLACE VIEW commits AS
    SELECT *, ingested_at FROM commits_raw FINAL;

CREATE OR REPLACE VIEW timeline_state AS
    SELECT *, ingested_at FROM timeline_state_raw FINAL;

CREATE OR REPLACE VIEW actions AS
    SELECT *, event_key, ingested_at FROM actions_raw FINAL;

CREATE OR REPLACE VIEW charges AS
    SELECT *, cost_usd, event_key, ingested_at FROM charges_raw FINAL;

-- take_rates: the reading T4.7 learns priors from.
--
-- T4.7 must report the sample size behind every prior. This view applies FINAL, so a
-- count() over it is the deduplicated truth and a resend never inflates it. Filter recency
-- on ingested_at, which the header explains and which prunes partitions.
--
-- chars_per_sec divides the spoken characters by the measured seconds. The WHERE clause
-- drops a take with no measured length, so the division never meets a zero.
--
-- A personal prior filters owner_id, language and speaker. A population prior drops the
-- owner_id filter and keeps the rest, which covers a creator with no history yet.
CREATE OR REPLACE VIEW take_rates AS
    SELECT
        owner_id,
        dub_id,
        language,
        speaker,
        voice,
        segment_index,
        attempt,
        take_id,
        text,
        slot_ms,
        measured_ms,
        delta_ms,
        repair,
        lengthUTF8(text) AS chars,
        lengthUTF8(text) / (measured_ms / 1000) AS chars_per_sec,
        created_at,
        ingested_at
    FROM takes_raw FINAL
    WHERE measured_ms > 0;

-- timeline_at_commit: state at a commit, as a query rather than a replay.
--
-- T4.5 calls this with three parameters, for example
-- SELECT * FROM timeline_at_commit(dub_id = 'd1', language = 'ml', commit_id = 'c4').
--
-- The recursive CTE walks parent_commit_id upward from the target commit, which gives the
-- exact ancestry and skips a sibling branch. The argMax then takes the newest snapshot per
-- segment across that ancestry. Rows are full snapshots, so the result carries every field
-- the newest ancestor commit knew, including the fields it did not change.
--
-- The view exists so no reader has to rebuild this walk by hand. Filtering on version_seq
-- or on branch instead returns the wrong ancestor set. See the commits_raw comment.
CREATE OR REPLACE VIEW timeline_at_commit AS
    WITH RECURSIVE ancestry AS
    (
        SELECT commit_id, parent_commit_id
        FROM commits_raw FINAL
        WHERE dub_id = {dub_id:String} AND commit_id = {commit_id:String}
        UNION ALL
        SELECT c.commit_id, c.parent_commit_id
        FROM commits_raw AS c FINAL
        INNER JOIN ancestry AS a ON c.commit_id = a.parent_commit_id
        WHERE c.dub_id = {dub_id:String}
    )
    SELECT
        segment_index,
        argMax(start_ms, version_seq)    AS start_ms,
        argMax(end_ms, version_seq)      AS end_ms,
        argMax(speaker, version_seq)     AS speaker,
        argMax(emotion, version_seq)     AS emotion,
        argMax(source_text, version_seq) AS source_text,
        argMax(text, version_seq)        AS text,
        argMax(take_id, version_seq)     AS take_id,
        max(version_seq)                 AS state_version_seq
    FROM timeline_state_raw FINAL
    WHERE dub_id = {dub_id:String}
      AND language = {language:String}
      AND commit_id IN (SELECT commit_id FROM ancestry)
    GROUP BY segment_index
    ORDER BY segment_index;
