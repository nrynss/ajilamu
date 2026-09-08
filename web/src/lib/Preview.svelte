<script module lang="ts">
  export interface DubbedAudioSource {
    language: string
    src: string
  }

  export interface PlaybackSnapshot {
    currentTime: number
    duration: number
    paused: boolean
  }
</script>

<script lang="ts">
  interface Props {
    src: string
    title?: string
    captionsSrc?: string
    durationMs?: number
    activeLanguage?: string
    dubbedAudio?: readonly DubbedAudioSource[]
    enableSpaceShortcut?: boolean
    onplaybackchange?: (snapshot: PlaybackSnapshot) => void
  }

  let {
    src,
    title = "Picture preview",
    captionsSrc,
    durationMs,
    activeLanguage,
    dubbedAudio = [],
    enableSpaceShortcut = true,
    onplaybackchange
  }: Props = $props()

  let video: HTMLVideoElement
  let audio: HTMLAudioElement | undefined
  let currentTime = $state(0)
  let duration = $state(0)
  let isPlaying = $state(false)
  let playbackError = $state("")

  let displayDuration = $derived(duration > 0 ? duration : (durationMs ?? 0) / 1000)
  let formattedCurrentTime = $derived(formatTime(currentTime))
  let formattedDuration = $derived(formatTime(displayDuration))
  let selectedAudio = $derived(dubbedAudio.find((track) => track.language === activeLanguage))

  $effect(() => {
    const track = selectedAudio
    const previousAudio = audio

    previousAudio?.pause()
    audio = undefined
    video.muted = Boolean(track)

    if (!track) {
      return
    }

    const nextAudio = new Audio(track.src)
    nextAudio.preload = "auto"
    nextAudio.currentTime = video.currentTime
    audio = nextAudio

    if (isPlaying) {
      void playDubbedAudio(nextAudio)
    }

    return () => {
      nextAudio.pause()
      if (audio === nextAudio) {
        audio = undefined
      }
    }
  })

  export function play(): Promise<void> {
    return video.play().catch((error: unknown) => {
      playbackError = "The picture could not start. Check that the video file is available."
      throw error
    })
  }

  export function pause(): void {
    video.pause()
    audio?.pause()
  }

  export function seek(seconds: number): void {
    video.currentTime = Math.max(0, Math.min(seconds, displayDuration))
  }

  function formatTime(seconds: number): string {
    if (!Number.isFinite(seconds) || seconds < 0) {
      return "0:00.0"
    }

    const minutes = Math.floor(seconds / 60)
    const remainingSeconds = seconds - minutes * 60
    return `${minutes}:${remainingSeconds.toFixed(1).padStart(4, "0")}`
  }

  function publishPlayback(): void {
    onplaybackchange?.({ currentTime, duration: displayDuration, paused: !isPlaying })
  }

  function syncAudioTime(): void {
    if (audio && Math.abs(audio.currentTime - video.currentTime) > 0.08) {
      audio.currentTime = video.currentTime
    }
  }

  async function playDubbedAudio(track: HTMLAudioElement): Promise<void> {
    try {
      await track.play()
    } catch {
      playbackError = "The dubbed audio could not start. The picture is still ready to play."
    }
  }

  function handleLoadedMetadata(): void {
    duration = Number.isFinite(video.duration) ? video.duration : 0
    playbackError = ""
    publishPlayback()
  }

  function handleTimeUpdate(): void {
    currentTime = video.currentTime
    syncAudioTime()
    publishPlayback()
  }

  function handlePlay(): void {
    isPlaying = true
    playbackError = ""
    syncAudioTime()
    if (audio) {
      void playDubbedAudio(audio)
    }
    publishPlayback()
  }

  function handlePause(): void {
    isPlaying = false
    audio?.pause()
    publishPlayback()
  }

  function handleSeeking(): void {
    syncAudioTime()
  }

  function handleError(): void {
    playbackError = "The picture could not load. Check that the video file is available."
  }

  function togglePlayback(): void {
    if (video.paused) {
      void play().catch(() => undefined)
    } else {
      pause()
    }
  }

  function isTypingTarget(target: EventTarget | null): boolean {
    return target instanceof HTMLElement && Boolean(
      target.closest("input, textarea, select, button, [contenteditable='true'], [role='textbox']")
    )
  }

  function handleSpace(event: KeyboardEvent): void {
    if (
      !enableSpaceShortcut ||
      event.key !== " " ||
      event.repeat ||
      event.isComposing ||
      event.altKey ||
      event.ctrlKey ||
      event.metaKey ||
      isTypingTarget(event.target)
    ) {
      return
    }

    event.preventDefault()
    togglePlayback()
  }
</script>

<svelte:window onkeydown={handleSpace} />

<section class="preview-player" aria-label={title}>
  <div class="video-frame">
    <!-- svelte-ignore a11y_media_has_caption -->
    <video
      bind:this={video}
      {src}
      aria-label={title}
      playsinline
      preload="metadata"
      onloadedmetadata={handleLoadedMetadata}
      ontimeupdate={handleTimeUpdate}
      onplay={handlePlay}
      onpause={handlePause}
      onseeking={handleSeeking}
      onerror={handleError}
    >
      {#if captionsSrc}
        <track kind="captions" src={captionsSrc} srclang="en" label="Captions" />
      {/if}
    </video>
  </div>

  <div class="controls" aria-label="Picture controls">
    <button
      type="button"
      aria-label={isPlaying ? "Pause picture" : "Play picture"}
      aria-pressed={isPlaying}
      onclick={togglePlayback}
    >
      {isPlaying ? "Pause" : "Play"}
    </button>
    <span class="time numeric" aria-label={`Current time ${formattedCurrentTime} of ${formattedDuration}`}>
      {formattedCurrentTime} / {formattedDuration}
    </span>
    <label class="scrubber">
      <span class="sr-only">Picture position</span>
      <input
        type="range"
        min="0"
        max={displayDuration || 0}
        step="0.01"
        value={currentTime}
        aria-valuetext={`${formattedCurrentTime} of ${formattedDuration}`}
        oninput={(event) => seek(Number(event.currentTarget.value))}
      />
    </label>
  </div>

  {#if playbackError}
    <p class="error" role="status">{playbackError}</p>
  {/if}
</section>

<style>
  .preview-player {
    min-width: 0;
  }

  .video-frame {
    aspect-ratio: 16 / 9;
    background: var(--sunken);
    border-radius: var(--radius-container);
    box-shadow: 0 8px 22px rgb(0 0 0 / 12%);
    overflow: hidden;
  }

  video {
    display: block;
    height: 100%;
    object-fit: contain;
    width: 100%;
  }

  .controls {
    align-items: center;
    display: flex;
    gap: 10px;
    min-height: 42px;
    padding-top: 9px;
  }

  button {
    font-size: 12.5px;
    min-width: 52px;
    padding: 5px 8px;
  }

  button[aria-pressed="true"] {
    background: var(--accent-q);
    border-color: var(--accent);
  }

  .time {
    color: var(--dim);
    font-size: 11.5px;
    white-space: nowrap;
  }

  .scrubber {
    display: flex;
    flex: 1;
    min-width: 56px;
  }

  .scrubber input {
    accent-color: var(--accent);
    cursor: pointer;
    width: 100%;
  }

  .error {
    color: var(--stop);
    font-size: 11.5px;
    margin: 0;
    padding-bottom: 8px;
  }

  .sr-only {
    height: 1px;
    margin: -1px;
    overflow: hidden;
    position: absolute;
    width: 1px;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
  }
</style>
