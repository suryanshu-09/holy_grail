import React from 'react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { DocumentCard } from '../components/DocumentCard';
import { listDocuments } from '../lib/api';

vi.mock('next/link', () => ({
  default: ({ href, children, className }: any) => (
    <a href={typeof href === 'string' ? href : String(href)} className={className}>
      {children}
    </a>
  ),
}));

describe('Document listing', () => {
  const realFetch = global.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    global.fetch = realFetch;
  });

  it('listDocuments hits /documents and returns rows (lib/api mock)', async () => {
    const rows = [
      { id: 'd1', filename: 'algebra-2020.pdf', subject: 'Math', year: 2020, status: 'processed' },
    ];
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(rows),
    } as any);
    const docs = await listDocuments({ subject: 'Math' });
    expect(docs).toEqual(rows);
    const url = (global.fetch as any).mock.calls[0][0] as string;
    expect(url).toContain('/documents');
    expect(url).toContain('subject=Math');
  });

  it('renders a document card with metadata and status', () => {
    render(
      <DocumentCard
        document={{
          id: 'd1',
          filename: 'algebra-2020.pdf',
          subject: 'Mathematics',
          year: 2020,
          status: 'processed',
          created_at: '2024-01-15T00:00:00Z',
        }}
      />
    );
    expect(screen.getByText('algebra-2020.pdf')).toBeInTheDocument();
    expect(screen.getByText('processed')).toBeInTheDocument();
    expect(screen.getByText('Mathematics')).toBeInTheDocument();
    expect(screen.getByText('2020')).toBeInTheDocument();
    expect(screen.getByRole('link')).toHaveAttribute('href', '/documents/d1');
  });

  it('renders a list of cards with placeholders for missing fields', () => {
    const docs = [
      { id: 'd1', filename: 'a.pdf', status: 'uploaded' },
      { id: 'd2', filename: 'b.pdf', subject: null, year: null, status: 'failed' },
    ];
    render(
      <div>
        {docs.map((d) => (
          <DocumentCard key={d.id} document={d} />
        ))}
      </div>
    );
    expect(screen.getByText('a.pdf')).toBeInTheDocument();
    expect(screen.getByText('b.pdf')).toBeInTheDocument();
    expect(screen.getByText('failed')).toBeInTheDocument();
    // Missing subject/year render as em-dash placeholders.
    expect(screen.getAllByText('—').length).toBeGreaterThan(0);
  });

  it('surfaces API errors from listDocuments', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Server Error',
      json: () => Promise.resolve({}),
    } as any);
    await expect(listDocuments()).rejects.toThrow(/Request failed: 500/);
    await waitFor(() => expect(global.fetch).toHaveBeenCalled());
  });
});
