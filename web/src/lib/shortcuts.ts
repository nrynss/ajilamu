export type SelectionDirection = "previous" | "next" | "left" | "right" | "up" | "down"

export interface ShortcutActions {
  showHelp?: () => void
  focusCommandBar?: () => void
  togglePlayback?: () => void
  moveSelection?: (direction: SelectionDirection) => void
  dismissModal?: () => void
}

export interface ShortcutManager {
  handle(event: KeyboardEvent): boolean
  start(): void
  stop(): void
}

const interactiveSelector = [
  "a[href]",
  "area[href]",
  "input",
  "textarea",
  "select",
  "button",
  "summary",
  "audio[controls]",
  "video[controls]",
  "[role='button']",
  "[role='checkbox']",
  "[role='combobox']",
  "[role='grid']",
  "[role='gridcell']",
  "[role='link']",
  "[role='listbox']",
  "[role='menu']",
  "[role='menubar']",
  "[role='menuitem']",
  "[role='menuitemcheckbox']",
  "[role='menuitemradio']",
  "[role='option']",
  "[role='radio']",
  "[role='radiogroup']",
  "[role='scrollbar']",
  "[role='searchbox']",
  "[role='spinbutton']",
  "[role='switch']",
  "[role='tab']",
  "[role='tablist']",
  "[role='textbox']",
  "[role='toolbar']",
  "[role='tree']",
  "[role='treegrid']",
  "[role='treeitem']",
  "[role='slider']"
].join(", ")

export function isEditableControl(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) {
    return false
  }

  const editable = target.closest<HTMLElement>("[contenteditable]")
  return Boolean(target.closest(interactiveSelector)) || Boolean(editable?.isContentEditable)
}

function hasShortcutModifier(event: KeyboardEvent): boolean {
  return event.altKey || event.ctrlKey || event.metaKey || event.isComposing
}

export function createShortcutManager(actions: ShortcutActions): ShortcutManager {
  function handle(event: KeyboardEvent): boolean {
    if (event.repeat || hasShortcutModifier(event) || isEditableControl(event.target)) {
      return false
    }

    let action: (() => void) | undefined
    let movesSelection = false

    switch (event.key) {
      case "?":
        action = actions.showHelp
        break
      case "/":
        action = actions.focusCommandBar
        break
      case " ":
      case "Spacebar":
        action = actions.togglePlayback
        break
      case "[":
        action = () => actions.moveSelection?.("previous")
        movesSelection = true
        break
      case "]":
        action = () => actions.moveSelection?.("next")
        movesSelection = true
        break
      case "ArrowLeft":
        action = () => actions.moveSelection?.("left")
        movesSelection = true
        break
      case "ArrowRight":
        action = () => actions.moveSelection?.("right")
        movesSelection = true
        break
      case "ArrowUp":
        action = () => actions.moveSelection?.("up")
        movesSelection = true
        break
      case "ArrowDown":
        action = () => actions.moveSelection?.("down")
        movesSelection = true
        break
      case "Escape":
        action = actions.dismissModal
        break
      default:
        return false
    }

    if (!action || (movesSelection && !actions.moveSelection)) {
      return false
    }

    event.preventDefault()
    action()
    return true
  }

  return {
    handle,
    start() {
      window.addEventListener("keydown", handle)
    },
    stop() {
      window.removeEventListener("keydown", handle)
    }
  }
}
