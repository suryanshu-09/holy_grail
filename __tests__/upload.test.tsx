import React from 'react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { Upload } from '../components/Upload';
import { uploadDocument } from '../lib/api';

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>();
  return { ...actual, uploadDocument: vi.fn() };
});

const mockedUpload = vi.mocked(uploadDocument);

function pdfFile(name = 'paper.pdf', size = 1024): File {
  const bytes = new Uint8Array(size);
  return new File([bytes], name, { type: 'application/pdf' });
}

describe('Upload flow', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the dropzone with PDF hint', () => {
    render(<Upload />);
    expect(screen.getByText(/Drag & drop a PDF here/i)).toBeInTheDocument();
    // The dropzone (role=button) wraps the inner "Choose File" button, so
    // multiple elements match — assert at least the inner button exists.
    expect(
      screen.getAllByRole('button', { name: /choose file/i }).length
    ).toBeGreaterThanOrEqual(1);
  });

  it('rejects non-PDF files without calling the API', async () => {
    render(<Upload />);
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    const bad = new File(['x'], 'notes.txt', { type: 'text/plain' });
    fireEvent.change(input, { target: { files: [bad] } });
    expect(await screen.findByRole('alert')).toHaveTextContent(/only PDF files are allowed/i);
    expect(mockedUpload).not.toHaveBeenCalled();
  });

  it('rejects empty PDF files', async () => {
    render(<Upload />);
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    const empty = new File([], 'empty.pdf', { type: 'application/pdf' });
    fireEvent.change(input, { target: { files: [empty] } });
    expect(await screen.findByRole('alert')).toHaveTextContent(/empty/i);
    expect(mockedUpload).not.toHaveBeenCalled();
  });

  it('uploads a valid PDF and calls onUploaded', async () => {
    const doc = { id: 'doc-1', filename: 'paper.pdf', status: 'uploaded' };
    mockedUpload.mockImplementation((_f, onProgress) => {
      onProgress?.(50);
      return Promise.resolve(doc);
    });
    const onUploaded = vi.fn();
    render(<Upload onUploaded={onUploaded} />);
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [pdfFile()] } });
    await waitFor(() => expect(mockedUpload).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(onUploaded).toHaveBeenCalledWith(doc));
  });

  it('shows a server error when upload fails', async () => {
    mockedUpload.mockRejectedValue(new Error('Upload failed: 400 Bad Request'));
    render(<Upload />);
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [pdfFile()] } });
    expect(await screen.findByRole('alert')).toHaveTextContent(/Upload failed/i);
  });
});
