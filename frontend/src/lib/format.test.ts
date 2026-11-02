import { describe, expect, it } from 'vitest'
import { checkFileSize, formatBytes, formatDuration, percent } from './format'

const GiB = 1024 ** 3

describe('format', () => {
  it('formats bytes', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(1536)).toBe('1.5 KiB')
    expect(formatBytes(100 * GiB)).toBe('100.0 GiB')
    expect(formatBytes(-1)).toBe('–')
  })
  it('formats durations', () => {
    expect(formatDuration(45)).toBe('45s')
    expect(formatDuration(125)).toBe('2m 05s')
    expect(formatDuration(3 * 3600 + 5 * 60)).toBe('3h 05m')
    expect(formatDuration(Infinity)).toBe('–')
  })
  it('clamps percentages', () => {
    expect(percent(5, 0)).toBe(0)
    expect(percent(150, 100)).toBe(100)
    expect(percent(25, 100)).toBe(25)
  })
  it('mirrors the server size rule: smaller than 100 GiB, not empty', () => {
    const max = 100 * GiB
    expect(checkFileSize(max - 1, max)).toBeNull()
    expect(checkFileSize(max, max)).toMatch(/smaller than/)
    expect(checkFileSize(max + 1, max)).toMatch(/smaller than/)
    expect(checkFileSize(0, max)).toMatch(/empty/)
  })
})
