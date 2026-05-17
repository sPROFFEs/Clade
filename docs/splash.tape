# Record the boot splash as a real animated GIF.
#
# Prereqs:
#   - vhs installed:  go install github.com/charmbracelet/vhs@latest
#                     (or: brew install vhs / apt install vhs)
#   - `clade` on PATH (run scripts/install.sh first)
#
# Then from the repo root:
#   vhs docs/splash.tape
#
# Output: docs/splash.gif — drop it into the README replacing the
# static "final frame" code block under ## Boot splash.

Output docs/splash.gif

# Visual settings tuned to make the wordmark render at a comfortable
# size without huge filesize. Adjust the width/height if your
# wordmark gets clipped on different terminal fonts.
Set Theme       "Tokyo Night"
Set FontSize    18
Set Width       1100
Set Height      520
Set Padding     20
Set TypingSpeed 50ms

# Open a fresh terminal that's already in the workspaces root so the
# splash transitions into the chat list rather than the first-run
# wizard. If you'd rather show the first-run wizard, point HOME at
# an empty dir instead.
Type "clade"
Enter

# The splash itself takes ~1.5s. Hold a beat longer so the chat
# list briefly appears, which proves the transition is clean.
Sleep 3s

# Quit so vhs has a stop point.
Ctrl+C
Sleep 500ms
