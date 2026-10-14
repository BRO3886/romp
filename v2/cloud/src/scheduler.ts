import { DurableObject } from 'cloudflare:workers';
import { Submission, configuration, type Bindings } from './config.js';
type Row = {
  id: string;
  submission: string;
  state: string;
  error: string;
  [key: string]: SqlStorageValue;
};
export class Scheduler extends DurableObject<Bindings> {
  constructor(ctx: DurableObjectState, env: Bindings) {
    super(ctx, env);
    ctx.storage.sql.exec(
      "CREATE TABLE IF NOT EXISTS queue (id TEXT PRIMARY KEY, submission TEXT NOT NULL, state TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', created INTEGER NOT NULL)",
    );
    ctx.storage.sql.exec(
      'CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value INTEGER NOT NULL)',
    );
  }
  async enqueue(input: Submission) {
    const value = Submission.parse(input);
    if (value.id !== `r${value.repositoryId}-i${value.issue.number}`)
      throw new Error('Run identity mismatch');
    this.ctx.storage.sql.exec(
      'INSERT OR IGNORE INTO queue (id, submission, state, created) VALUES (?, ?, ?, ?)',
      value.id,
      JSON.stringify(value),
      'queued',
      Date.now(),
    );
    await this.ctx.storage.setAlarm(Date.now() + 100);
    return value.id;
  }
  async configure(concurrency: number) {
    if (!Number.isSafeInteger(concurrency) || concurrency < 1 || concurrency > 32)
      throw new Error('Invalid concurrency');
    this.ctx.storage.sql.exec(
      "INSERT INTO settings VALUES ('concurrency', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
      concurrency,
    );
    await this.ctx.storage.setAlarm(Date.now() + 100);
    return this.status();
  }
  status() {
    const setting = this.ctx.storage.sql
      .exec<{ value: number }>("SELECT value FROM settings WHERE key='concurrency'")
      .toArray()[0];
    return {
      concurrency: setting?.value ?? configuration(this.env).concurrency,
      runs: this.ctx.storage.sql
        .exec<Row>('SELECT id, submission, state, error FROM queue ORDER BY created, id')
        .toArray()
        .map(({ id, state, error }) => ({ id, state, error })),
    };
  }
  async cancel(id: string) {
    const row = this.ctx.storage.sql.exec<Row>('SELECT * FROM queue WHERE id=?', id).toArray()[0];
    if (!row) throw new Error('Unknown run');
    if (row.state === 'queued')
      this.ctx.storage.sql.exec("UPDATE queue SET state='finished' WHERE id=?", id);
    else if (row.state === 'active') await this.env.RUNS.getByName(id).cancel();
    await this.ctx.storage.setAlarm(Date.now() + 100);
  }
  override async alarm() {
    await this.ctx.storage.setAlarm(Date.now() + 15_000);
    const rows = this.ctx.storage.sql
      .exec<Row>("SELECT * FROM queue WHERE state='active' ORDER BY created, id")
      .toArray();
    for (const row of rows) {
      try {
        const run = this.env.RUNS.getByName(row.id);
        await run.start(Submission.parse(JSON.parse(row.submission) as unknown));
        const status = await run.status();
        if (status.state === 'finished')
          this.ctx.storage.sql.exec(
            "UPDATE queue SET state='finished', error='' WHERE id=?",
            row.id,
          );
        else this.ctx.storage.sql.exec("UPDATE queue SET error='' WHERE id=?", row.id);
      } catch (error) {
        this.ctx.storage.sql.exec(
          'UPDATE queue SET error=? WHERE id=?',
          error instanceof Error ? error.message : 'Dispatch failed',
          row.id,
        );
      }
    }
    this.ctx.storage.transactionSync(() => {
      const active = this.ctx.storage.sql
        .exec<{ n: number }>("SELECT COUNT(*) AS n FROM queue WHERE state='active'")
        .one().n;
      const available = Math.max(0, this.status().concurrency - active);
      this.ctx.storage.sql.exec(
        "UPDATE queue SET state='active' WHERE id IN (SELECT id FROM queue WHERE state='queued' ORDER BY created, id LIMIT ?)",
        available,
      );
    });
    for (const row of this.ctx.storage.sql
      .exec<Row>("SELECT * FROM queue WHERE state='active'")
      .toArray()) {
      try {
        await this.env.RUNS.getByName(row.id).start(
          Submission.parse(JSON.parse(row.submission) as unknown),
        );
      } catch (error) {
        this.ctx.storage.sql.exec(
          'UPDATE queue SET error=? WHERE id=?',
          error instanceof Error ? error.message : 'Dispatch failed',
          row.id,
        );
      }
    }
    if (
      !this.ctx.storage.sql
        .exec<{ n: number }>("SELECT COUNT(*) AS n FROM queue WHERE state!='finished'")
        .one().n
    )
      await this.ctx.storage.deleteAlarm();
  }
}
