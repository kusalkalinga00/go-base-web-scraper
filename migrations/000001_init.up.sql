CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY,
    url TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT 'scrape'
        CHECK (mode IN ('scrape', 'crawl', 'http')),
    page_limit INTEGER DEFAULT 10,
    wait_seconds INTEGER DEFAULT 3,
    main_content BOOLEAN DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    error TEXT,
    pages_crawled INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS results (
    id BIGSERIAL PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    markdown TEXT NOT NULL,
    title TEXT,
    description TEXT,
    language TEXT,
    canonical_url TEXT,
    og_image TEXT,
    favicon TEXT,
    word_count INTEGER DEFAULT 0,
    links_internal INTEGER DEFAULT 0,
    links_external INTEGER DEFAULT 0,
    images_count INTEGER DEFAULT 0,
    headings JSONB NOT NULL DEFAULT '[]'::jsonb,
    response_time_ms INTEGER,
    used_proxy BOOLEAN DEFAULT FALSE,
    crawled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    assets JSONB NOT NULL DEFAULT '[]'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_created ON jobs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_results_job ON results(job_id);
