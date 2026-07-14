export function isAudioFilename(filename: string): boolean {
  return filename.toLowerCase().endsWith('.mp3');
}
