#!/bin/bash
#
# Release script for gonkey
# Creates a new semantic version tag and GitHub release
#
# Usage: ./dev/tools/release.sh [--dry-run] [--generate-notes]
#
# Options:
#   --dry-run        Show what would be done without making changes
#   --generate-notes Use Claude to generate release notes (requires claude CLI)
#

set -euo pipefail

# Disable git pager to prevent 'less' from blocking script execution in TTY
export GIT_PAGER=cat

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Script options
DRY_RUN=false
GENERATE_NOTES=false

# Parse arguments
for arg in "$@"; do
    case $arg in
        --dry-run)
            DRY_RUN=true
            ;;
        --generate-notes)
            GENERATE_NOTES=true
            ;;
        -h|--help)
            echo "Usage: $0 [--dry-run] [--generate-notes]"
            echo ""
            echo "Options:"
            echo "  --dry-run        Show what would be done without making changes"
            echo "  --generate-notes Use Claude to generate release notes"
            exit 0
            ;;
    esac
done

# Helper functions
info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Check prerequisites
check_prerequisites() {
    info "Checking prerequisites..."

    # Check if we're in a git repository
    if ! git rev-parse --git-dir > /dev/null 2>&1; then
        error "Not in a git repository"
    fi

    # Check if gh is installed
    if ! command -v gh &> /dev/null; then
        error "GitHub CLI (gh) is not installed. Install it: https://cli.github.com/"
    fi

    # Check if gh is authenticated
    if ! gh auth status &> /dev/null; then
        error "GitHub CLI is not authenticated. Run: gh auth login"
    fi

    # Check for uncommitted changes
    if ! git diff-index --quiet HEAD -- 2>/dev/null; then
        error "You have uncommitted changes. Please commit or stash them first."
    fi

    success "All prerequisites met"
}

# Global variable for main branch
MAIN_BRANCH=""

# Detect main branch (main or master) on remote
detect_main_branch() {
    # Try to detect default branch from remote
    local default_branch
    default_branch=$(git remote show origin 2>/dev/null | grep 'HEAD branch' | awk '{print $NF}')

    if [[ -n "$default_branch" ]]; then
        echo "$default_branch"
        return
    fi

    # Fallback: check if main or master exists
    if git rev-parse --verify origin/main &>/dev/null; then
        echo "main"
    elif git rev-parse --verify origin/master &>/dev/null; then
        echo "master"
    else
        error "Could not detect main branch (tried main, master)"
    fi
}

# Fetch and setup main branch reference
setup_main_branch() {
    info "Fetching latest changes from remote..."
    git fetch --tags origin

    MAIN_BRANCH=$(detect_main_branch)
    info "Using main branch: $MAIN_BRANCH"

    local current_branch
    current_branch=$(git rev-parse --abbrev-ref HEAD)

    if [[ "$current_branch" != "$MAIN_BRANCH" ]]; then
        info "Current branch: $current_branch (release will be created from origin/$MAIN_BRANCH)"
    fi
}

# Check if there are new commits since last tag on main branch
check_new_commits_since_tag() {
    local latest_tag="$1"

    if [[ -z "$latest_tag" || "$latest_tag" == "v0.0.0" ]]; then
        # No previous tag, check if main branch has any commits
        return 0
    fi

    # Get the commit that the tag points to
    local tag_commit
    tag_commit=$(git rev-list -n 1 "$latest_tag" 2>/dev/null || echo "")

    # Get the latest commit on main branch
    local main_commit
    main_commit=$(git rev-parse "origin/$MAIN_BRANCH" 2>/dev/null || echo "")

    if [[ -z "$main_commit" ]]; then
        error "Could not get latest commit from origin/$MAIN_BRANCH"
    fi

    if [[ "$tag_commit" == "$main_commit" ]]; then
        error "No new commits since $latest_tag on origin/$MAIN_BRANCH. Release already exists for this commit."
    fi

    # Count commits since tag on main branch
    local commit_count
    commit_count=$(git rev-list --count "${latest_tag}..origin/${MAIN_BRANCH}" 2>/dev/null || echo "0")

    if [[ "$commit_count" -eq 0 ]]; then
        error "No new commits since $latest_tag on origin/$MAIN_BRANCH. Nothing to release."
    fi

    info "Found $commit_count new commit(s) since $latest_tag on origin/$MAIN_BRANCH"
}

# Get the latest tag
get_latest_tag() {
    local tag
    tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
    echo "$tag"
}

# Show commits since last tag on main branch
show_commits_since_tag() {
    local tag="$1"

    echo ""
    info "Commits since $tag on origin/$MAIN_BRANCH:"
    echo "---"

    if [[ -n "$tag" && "$tag" != "v0.0.0" ]]; then
        git log --oneline --no-decorate "${tag}..origin/${MAIN_BRANCH}"
    else
        git log --oneline --no-decorate "origin/${MAIN_BRANCH}" -20
        echo "... (showing last 20 commits, no previous tag found)"
    fi

    echo "---"
    echo ""
}

# Interactive version input
prompt_version() {
    local current_version="$1"

    # All prompts go to stderr so only the result goes to stdout
    echo "" >&2
    echo -e "${BLUE}[INFO]${NC} Semantic versioning guide:" >&2
    echo "  • patch (X.Y.Z+1) - bug fixes, minor updates" >&2
    echo "  • minor (X.Y+1.0) - new features, backward compatible" >&2
    echo "  • major (X+1.0.0) - breaking changes" >&2
    echo "" >&2
    echo -e "${BLUE}[INFO]${NC} Current version: ${current_version}" >&2
    echo "" >&2

    while true; do
        read -p "Enter new version (e.g. 0.3.0 or v0.3.0): " -r </dev/tty

        # Remove 'v' prefix if present (will be added back later)
        local version="${REPLY#v}"

        # Validate version format (X.Y.Z)
        if [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            echo "$version"
            return
        else
            echo -e "${YELLOW}[WARN]${NC} Invalid format. Use X.Y.Z (e.g. 0.3.0, 1.0.0)" >&2
        fi
    done
}

# Generate release notes using Claude
generate_release_notes_with_claude() {
    local previous_tag="$1"
    local new_tag="$2"

    if ! command -v claude &> /dev/null; then
        warn "Claude CLI is not installed. Skipping AI-generated notes."
        return 1
    fi

    info "Generating release notes with Claude..."

    local commits
    if [[ -n "$previous_tag" && "$previous_tag" != "v0.0.0" ]]; then
        commits=$(git log --oneline "${previous_tag}..origin/${MAIN_BRANCH}")
    else
        commits=$(git log --oneline "origin/${MAIN_BRANCH}" -20)
    fi

    local prompt="Generate release notes for version ${new_tag} based on these commits.
Write only in English, regardless of commit message language.
Use markdown format with sections: ## Description, ## Fixes (if any), ## Improvements (if any).
Keep it brief and professional. Here are the commits:

${commits}"

    # Using claude CLI with print flag to get output
    local notes
    notes=$(echo "$prompt" | claude --print 2>/dev/null) || return 1

    echo "$notes"
}

# Generate basic release notes
generate_basic_release_notes() {
    local previous_tag="$1"
    local new_tag="$2"

    echo "## Description"
    echo ""
    echo "Release ${new_tag}"
    echo ""
    echo "## Improvements"
    echo ""

    if [[ -n "$previous_tag" && "$previous_tag" != "v0.0.0" ]]; then
        git log --oneline "${previous_tag}..origin/${MAIN_BRANCH}" | while read -r line; do
            echo "* $line"
        done
        echo ""
        echo "**Full Changelog**: https://github.com/$(gh repo view --json nameWithOwner -q '.nameWithOwner')/compare/${previous_tag}...${new_tag}"
    else
        git log --oneline "origin/${MAIN_BRANCH}" -10 | while read -r line; do
            echo "* $line"
        done
    fi
}

# Create release
create_release() {
    local new_tag="$1"
    local release_notes="$2"

    # Get the commit hash from main branch
    local main_commit
    main_commit=$(git rev-parse "origin/$MAIN_BRANCH")

    if [[ "$DRY_RUN" == "true" ]]; then
        info "[DRY-RUN] Would create tag: $new_tag on commit $main_commit (origin/$MAIN_BRANCH)"
        info "[DRY-RUN] Would create release with notes:"
        echo "$release_notes"
        return
    fi

    # Create and push tag on main branch commit
    info "Creating tag $new_tag on origin/$MAIN_BRANCH ($main_commit)..."
    git tag -a "$new_tag" -m "Release $new_tag" "$main_commit"

    info "Pushing tag to remote..."
    git push origin "$new_tag"

    # Create GitHub release
    info "Creating GitHub release..."
    echo "$release_notes" | gh release create "$new_tag" \
        --title "$new_tag" \
        --notes-file -

    success "Release $new_tag created successfully!"
    echo ""
    echo "View release: $(gh release view "$new_tag" --json url -q '.url')"
}

# Main function
main() {
    echo ""
    echo "======================================="
    echo "       gonkey Release"
    echo "======================================="
    echo ""

    if [[ "$DRY_RUN" == "true" ]]; then
        warn "Running in DRY-RUN mode - no changes will be made"
        echo ""
    fi

    # Run checks
    check_prerequisites
    setup_main_branch

    # Get latest tag
    local latest_tag
    latest_tag=$(get_latest_tag)

    if [[ -n "$latest_tag" ]]; then
        info "Latest tag: $latest_tag"
    else
        warn "No existing tags found. Starting from v0.0.0"
        latest_tag="v0.0.0"
    fi

    # Check if there are new commits to release
    check_new_commits_since_tag "$latest_tag"

    # Get current version (without 'v' prefix)
    local current_version="${latest_tag#v}"
    info "Current version: ${current_version}"

    # Show recent commits
    show_commits_since_tag "$latest_tag"

    # Prompt user for new version
    local new_version
    new_version=$(prompt_version "$current_version")

    # Add 'v' prefix for tag
    local new_tag="v${new_version}"

    echo ""
    info "New version will be: $new_tag"
    echo ""

    # Generate release notes
    local release_notes
    if [[ "$GENERATE_NOTES" == "true" ]]; then
        release_notes=$(generate_release_notes_with_claude "$latest_tag" "$new_tag") || \
            release_notes=$(generate_basic_release_notes "$latest_tag" "$new_tag")
    else
        release_notes=$(generate_basic_release_notes "$latest_tag" "$new_tag")
    fi

    echo "Release notes preview:"
    echo "---"
    echo "$release_notes"
    echo "---"
    echo ""

    # In dry-run mode, skip confirmation and just show what would happen
    if [[ "$DRY_RUN" == "true" ]]; then
        info "[DRY-RUN] Skipping confirmation (no changes will be made)"
        create_release "$new_tag" "$release_notes"
        return
    fi

    # Confirm (only in real mode)
    read -p "Create release $new_tag? (y/N): " -n 1 -r </dev/tty
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        error "Aborted"
    fi

    # Create the release
    create_release "$new_tag" "$release_notes"
}

# Run main
main "$@"
