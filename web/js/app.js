// Page-level DOM glue for the Excel CLI tournament creator. Imports pure-logic
// helpers from validation.js / seeding.js / time_estimator.js / api.js and wires
// them to event listeners, localStorage persistence, and Bootstrap modals.

import {
    escapeHtml,
    getIssueLineNumber,
    sanitizeNameForValidation,
    getParticipantValidationState,
    validateCourtsValue,
    validatePoolSettings
} from './validation.js';
import {
    formatDuration,
    formatTime,
    estimateSchedule,
    parseStartTime,
    fetchScheduleEstimate
} from './time_estimator.js';
import { validateSeedAssignments } from './seeding.js';
import {
    fetchAppStatus,
    fetchDownloadStatus,
    parseParticipants
} from './api.js';
import { startDownloadPoll } from './download_polling.js';

// Fetch application version from API
async function fetchAppVersion() {
    try {
        const data = await fetchAppStatus();
        const versionBadge = document.getElementById('app-version');
        versionBadge.textContent = `${data.version}`;
        versionBadge.title = `Build: ${data.buildDate}\n${data.osArch}\n${data.goVersion}`;

        // Initialize tooltip for version badge
        new bootstrap.Tooltip(versionBadge, {
            placement: 'bottom',
            html: true
        });
    } catch (error) {
        console.error('Failed to fetch version:', error);
        document.getElementById('app-version').textContent = 'v?.?.?';
        showToast('Could not connect to the server to fetch version info.', 'warning');
    }
}

// Theme toggle functionality
const themeToggle = document.getElementById('themeToggle');

// Check for saved theme preference or use preferred color scheme
const savedTheme = localStorage.getItem('theme');
if (savedTheme === 'dark' || (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
    document.body.setAttribute('data-theme', 'dark');
    themeToggle.checked = true;
}

// Toggle theme
themeToggle.addEventListener('change', () => {
    if (themeToggle.checked) {
        document.body.setAttribute('data-theme', 'dark');
        localStorage.setItem('theme', 'dark');
    } else {
        document.body.removeAttribute('data-theme');
        localStorage.setItem('theme', 'light');
    }
});

// Form validation
function validateForm() {
    const playerList = document.getElementById('playerList').value.trim();
    const errorMsg = document.getElementById('error-message');
    let isValid = true;

    errorMsg.style.display = 'none';

    // Check if player list is empty
    if (playerList === '') {
        errorMsg.textContent = 'Error: Player list cannot be empty';
        errorMsg.style.display = 'block';
        isValid = false;
    }

    const participantValidation = validateParticipantListInput();
    if (participantValidation.errors.length > 0) {
        errorMsg.textContent = `Error: Fix the participant list before creating the tournament. ${participantValidation.errors[0]}`;
        errorMsg.style.display = 'block';
        playerListValidation.scrollIntoView({ behavior: 'smooth', block: 'center' });
        isValid = false;
    }

    if (document.getElementById('pools').checked) {
        const winnersPerPool = parseInt(document.getElementById('winnersPerPool').value);
        const playersPerPool = parseInt(document.getElementById('playersPerPool').value);
        const isMaxMode = document.getElementById('poolSizeMax').checked;

        const poolCheck = validatePoolSettings(winnersPerPool, playersPerPool, isMaxMode);
        if (!poolCheck.ok) {
            errorMsg.textContent = `Error: ${poolCheck.error}`;
            errorMsg.style.display = 'block';
            isValid = false;
        }
    }

    // Courts: A–Z labels, hard-cap at 26.
    const courtsCheck = validateCourtsValue(document.getElementById('courts').value);
    if (!courtsCheck.ok) {
        errorMsg.textContent = `Error: ${courtsCheck.error}`;
        errorMsg.style.display = 'block';
        isValid = false;
    }

    if (isValid) {
        const lines = playerList.split('\n').filter(line => line.trim() !== '');
        const tournamentType = document.getElementById('pools').checked ? 'Pools and Playoffs' : 'Playoffs';

        // Show confirmation dialog
        if (confirm(`You are about to create a ${tournamentType} tournament with ${lines.length} participants.\nAre you sure you want to continue?`)) {
            // Generate unique download token
            const downloadToken = 'download_' + new Date().getTime() + '_' + Math.random().toString(36).substring(7);

            // Add token as hidden field
            let tokenInput = document.getElementById('downloadToken');
            if (!tokenInput) {
                tokenInput = document.createElement('input');
                tokenInput.type = 'hidden';
                tokenInput.id = 'downloadToken';
                tokenInput.name = 'downloadToken';
                document.getElementById('tournamentForm').appendChild(tokenInput);
            }
            tokenInput.value = downloadToken;

            // Show loading overlay
            document.getElementById('loading-overlay').classList.add('active');

            // Start polling for download completion
            checkDownloadComplete(downloadToken);

            return true;
        } else {
            return false;
        }
    } else {
        return false;
    }
}

// Expose validateForm to inline `onsubmit="return validateForm()"` attribute
// in index.html. Module scripts have their own scope, so we must attach it
// explicitly to window.
window.validateForm = validateForm;

function hideLoadingOverlay() {
    document.getElementById('loading-overlay').classList.remove('active');
}

function checkDownloadComplete(downloadToken) {
    startDownloadPoll(downloadToken, {
        fetchStatus: fetchDownloadStatus,
        onReady: hideLoadingOverlay,
        onError: () => {
            hideLoadingOverlay();
            showToast('Lost connection while waiting for the tournament file. Please try again.', 'danger');
        },
        onTimeout: () => {
            hideLoadingOverlay();
            showToast('Tournament generation timed out. The file may be too large or the server may be busy.', 'warning');
        },
    });
}

// Tournament type toggle functionality
document.getElementById('playoffs').addEventListener('change', function () {
    document.getElementById('poolOptionsSection').style.display = 'none';
});

document.getElementById('pools').addEventListener('change', function () {
    document.getElementById('poolOptionsSection').style.display = 'block';
});

// Initialize tooltips
var tooltipTriggerList = [].slice.call(document.querySelectorAll('[data-bs-toggle="tooltip"]'))
var tooltipList = tooltipTriggerList.map(function (tooltipTriggerEl) {
    return new bootstrap.Tooltip(tooltipTriggerEl)
})

const playerListTextarea = document.getElementById('playerList');
const playerListHelp = document.getElementById('playerListHelp');
const zekkenCheckbox = document.getElementById('withZekkenName');
const csvFormatColumns = document.getElementById('csvFormatColumns');
const csvFormatDescription = document.getElementById('csvFormatDescription');
const csvFormatExample = document.getElementById('csvFormatExample');
const playerListLineNumbers = document.getElementById('playerListLineNumbers');
const playerListValidation = document.getElementById('playerListValidation');
const seedsDisplayNameHeader = document.getElementById('seedsDisplayNameHeader');
const defaultPlayerListPlaceholder = playerListTextarea.placeholder;
const defaultPlayerListHelpHtml = playerListHelp.innerHTML;
let validationDebounceTimer = null;

function renderIssueListItem(issue) {
    const lineNumber = getIssueLineNumber(issue.message);
    const escapedSeverity = escapeHtml(issue.severity);
    const escapedMessage = escapeHtml(issue.message);

    if (lineNumber === null) {
        return `<li><strong>${escapedSeverity}:</strong> ${escapedMessage}</li>`;
    }

    const lineLabel = `Line ${lineNumber}`;
    const escapedLineLabel = escapeHtml(lineLabel);
    const lineButton = `<button type="button" class="validation-line-jump" data-line-number="${lineNumber}" aria-label="Jump to participant list ${escapedLineLabel}">${escapedLineLabel}</button>`;
    const messageWithJump = escapedMessage.replace(escapedLineLabel, lineButton);
    return `<li><strong>${escapedSeverity}:</strong> ${messageWithJump}</li>`;
}

function getLineStartIndex(lineNumber) {
    const lines = playerListTextarea.value.split('\n');
    const boundedLineNumber = Math.min(Math.max(lineNumber, 1), Math.max(lines.length, 1));
    let index = 0;
    for (let i = 0; i < boundedLineNumber - 1; i += 1) {
        index += (lines[i] || '').length + 1;
    }
    return { index, boundedLineNumber };
}

function jumpToParticipantLine(lineNumber) {
    const { index, boundedLineNumber } = getLineStartIndex(lineNumber);
    playerListTextarea.focus();
    playerListTextarea.setSelectionRange(index, index);

    const lineHeight = parseFloat(window.getComputedStyle(playerListTextarea).lineHeight) || 24;
    playerListTextarea.scrollTop = Math.max(0, ((boundedLineNumber - 1) * lineHeight) - (2 * lineHeight));
    playerListLineNumbers.scrollTop = playerListTextarea.scrollTop;
}

function updateLineNumbers() {
    const lineCount = Math.max(playerListTextarea.value.split('\n').length, 1);
    const lineLabels = Array.from({ length: lineCount }, (_, index) => index + 1).join('\n');
    playerListLineNumbers.textContent = lineLabels;
    playerListLineNumbers.scrollTop = playerListTextarea.scrollTop;
}

function renderFormatGuide(columns, description, example) {
    csvFormatColumns.innerHTML = columns
        .map((column, index) => `<span class="badge text-bg-light border">Column ${index + 1}: ${column}</span>`)
        .join('');
    csvFormatDescription.innerHTML = description;
    csvFormatExample.textContent = `Example: ${example}`;
}

function renderPlayerListValidation(state) {
    playerListValidation.className = 'validation-panel';

    if (state.isEmpty) {
        playerListValidation.innerHTML = '';
        return;
    }

    const issues = [];
    const maxIssuesToShow = 5;
    const combinedIssues = [
        ...state.errors.map(message => ({ severity: 'Error', message })),
        ...state.warnings.map(message => ({ severity: 'Warning', message })),
        ...state.infos.map(message => ({ severity: 'Info', message }))
    ];

    combinedIssues.slice(0, maxIssuesToShow).forEach(issue => {
        issues.push(renderIssueListItem(issue));
    });

    if (combinedIssues.length > maxIssuesToShow) {
        issues.push(`<li>And ${combinedIssues.length - maxIssuesToShow} more issue(s)…</li>`);
    }

    if (state.errors.length > 0) {
        playerListValidation.classList.add('validation-error');
        playerListValidation.innerHTML = `
            <div class="validation-panel-title">Participant list needs attention</div>
            <div class="small mb-2">Fix these issues before creating the tournament or assigning seeds.</div>
            <ul>${issues.join('')}</ul>
        `;
        return;
    }

    if (state.warnings.length > 0) {
        playerListValidation.classList.add('validation-warning');
        playerListValidation.innerHTML = `
            <div class="validation-panel-title">Participant list looks usable, with a few caveats</div>
            <div class="small mb-2">${state.participantCount} participant(s) parsed for the current mode.</div>
            <ul>${issues.join('')}</ul>
        `;
        return;
    }

    if (state.infos.length > 0) {
        playerListValidation.classList.add('validation-info');
        playerListValidation.innerHTML = `
            <div class="validation-panel-title">Participant list format looks good</div>
            <div class="small mb-2">${state.participantCount} participant(s) match the current ${state.withZekkenName ? 'Zekken' : 'standard'} CSV format.</div>
            <ul>${issues.join('')}</ul>
        `;
        return;
    }

    playerListValidation.classList.add('validation-success');
    playerListValidation.innerHTML = `
        <div class="validation-panel-title">Participant list format looks good</div>
        <div class="small">${state.participantCount} participant(s) match the current ${state.withZekkenName ? 'Zekken' : 'standard'} CSV format.</div>
    `;
}

function validateParticipantListInput(options = {}) {
    const settings = {
        render: true,
        ...options
    };
    const state = getParticipantValidationState(playerListTextarea.value, zekkenCheckbox.checked);
    if (settings.render) {
        renderPlayerListValidation(state);
    }
    return state;
}

function scheduleParticipantValidation() {
    if (validationDebounceTimer) {
        clearTimeout(validationDebounceTimer);
    }

    validationDebounceTimer = setTimeout(() => {
        validateParticipantListInput();
    }, 200);
}

function updateZekkenHints() {
    if (zekkenCheckbox.checked) {
        playerListTextarea.placeholder =
            'Enter one participant per line (no header row):\n' +
            'First_Name1 Last_Name1, ZekkenName1, Dojo1\n' +
            'First_Name2 Last_Name2, ZekkenName2, Dojo2\n' +
            '...';
        playerListHelp.innerHTML =
            '<i class="bi bi-info-circle"></i> Use one participant per line with no header row. In Zekken mode, enter <strong>Name, ZekkenName, Dojo</strong>. Leave the Zekken name blank only if you want it to fall back to a shortened display name.';
        renderFormatGuide(
            ['Name', 'ZekkenName', 'Dojo'],
            'Headerless CSV with three columns. Column 2 becomes the display name shown on the zekken.',
            'Jane Doe, ジェーン, Enzan Dojo'
        );
    } else {
        playerListTextarea.placeholder = defaultPlayerListPlaceholder;
        playerListHelp.innerHTML = defaultPlayerListHelpHtml;
        renderFormatGuide(
            ['Name', 'Dojo'],
            'Headerless CSV with two columns. Column 2 is used to help separate same-dojo participants in pools.',
            'Jane Doe, Enzan Dojo'
        );
    }
}

function updatePoolSizeHints() {
    const playersPerPoolLabel = document.querySelector('label[for="playersPerPool"]');
    const isMaxMode = document.getElementById('poolSizeMax').checked;

    if (isMaxMode) {
        playersPerPoolLabel.textContent = 'Maximum players/teams per pool:';
    } else {
        playersPerPoolLabel.textContent = 'Minimum players/teams per pool:';
    }
}

// Load saved values from localStorage
window.addEventListener('DOMContentLoaded', () => {
    // Tournament type
    const savedTournamentType = localStorage.getItem('tournamentType');
    if (savedTournamentType) {
        const el = document.getElementById(savedTournamentType);
        if (el) {
            el.checked = true;
            if (savedTournamentType === 'pools') {
                document.getElementById('poolOptionsSection').style.display = 'block';
            }
        }
    }

    // Pool size mode
    const savedPoolSizeMode = localStorage.getItem('poolSizeMode');
    if (savedPoolSizeMode) {
        if (savedPoolSizeMode === 'max') {
            document.getElementById('poolSizeMax').checked = true;
        } else {
            document.getElementById('poolSizeMin').checked = true;
        }
    }

    // Other form values
    const formElements = {
        'singleTree': 'checked',
        'withZekkenName': 'checked',
        'determined': 'checked',
        'teamMatches': 'value',
        'winnersPerPool': 'value',
        'playersPerPool': 'value',
        'courts': 'value',
        'roundRobin': 'checked',
        'playerList': 'value'
    };

    Object.entries(formElements).forEach(([id, prop]) => {
        const savedValue = localStorage.getItem(id);
        if (savedValue !== null) {
            const el = document.getElementById(id);
            if (el) {
                if (prop === 'checked') {
                    el.checked = savedValue === 'true';
                } else {
                    el.value = savedValue;
                }
            }
        }
    });

    // Fix for the playerList typo in the old version
    const oldPlayerList = localStorage.getItem('teplayerListxt');
    if (oldPlayerList) {
        document.getElementById('playerList').value = oldPlayerList;
        localStorage.setItem('playerList', oldPlayerList);
        localStorage.removeItem('teplayerListxt');
    }

    updateZekkenHints();
    updatePoolSizeHints();
    updateLineNumbers();
    validateParticipantListInput();
});

// Save values to localStorage on input change
document.querySelectorAll('input, textarea').forEach(function (el) {
    el.addEventListener('input', function () {
        if (this.type === 'radio' && !this.checked) {
            return;
        }
        const key = (this.type === 'radio' && this.name) ? this.name : (this.id || this.name);
        localStorage.setItem(key, this.type === 'checkbox' ? this.checked : this.value);
    });
});

document.getElementById('poolSizeMin').addEventListener('change', updatePoolSizeHints);
document.getElementById('poolSizeMax').addEventListener('change', updatePoolSizeHints);

// Sample data loading functions
const sampleData = {
    small: `Kevin Clark, Team Alpha
Luke Rodriguez, Team Beta
Michael Lewis, Team Gamma
Nathan Lee, Team Delta
Oliver Walker, Team Epsilon
Paul Hall, Team Alpha
Quentin Allen, Team Beta
Robert Young, Team Gamma
Steven Hernandez, Team Delta
Thomas King, Team Epsilon`,
    medium: `Kevin Clark, Team Alpha
Luke Rodriguez, Team Beta
Michael Lewis, Team Gamma
Nathan Lee, Team Delta
Oliver Walker, Team Epsilon
Paul Hall, Team Alpha
Quentin Allen, Team Beta
Robert Young, Team Gamma
Steven Hernandez, Team Delta
Thomas King, Team Epsilon
Ulysses Garcia, Team Alpha
Vincent Moore, Team Beta
William Wright, Team Gamma
Xavier Thompson, Team Delta
Yusuf Robinson, Team Epsilon
Zachary Scott, Team Alpha
Adrian Hill, Team Beta
Benjamin Carter, Team Gamma`,
    large: `Kevin Clark, Team Alpha
Luke Rodriguez, Team Beta
Michael Lewis, Team Gamma
Nathan Lee, Team Delta
Oliver Walker, Team Epsilon
Paul Hall, Team Alpha
Quentin Allen, Team Beta
Robert Young, Team Gamma
Steven Hernandez, Team Delta
Thomas King, Team Epsilon
Ulysses Garcia, Team Alpha
Vincent Moore, Team Beta
William Wright, Team Gamma
Xavier Thompson, Team Delta
Yusuf Robinson, Team Epsilon
Zachary Scott, Team Alpha
Adrian Hill, Team Beta
Benjamin Carter, Team Gamma
Charles Phillips, Team Delta
Daniel Evans, Team Epsilon
Edward Collins, Team Alpha
Frank Torres, Team Beta
George Jenkins, Team Gamma
Henry Turner, Team Delta
Isaac Parker, Team Epsilon
Jack Adams, Team Alpha`
};

function toZekkenDisplayName(name) {
    const trimmedName = name.trim();
    if (!trimmedName) {
        return '';
    }

    const firstName = trimmedName.split(/\s+/)[0] || trimmedName;
    return firstName.toUpperCase();
}

function toZekkenSample(sampleText) {
    return sampleText
        .split('\n')
        .map(line => line.trim())
        .filter(line => line !== '')
        .map(line => {
            const parts = line.split(',').map(part => part.trim());
            const name = parts[0] || '';
            const dojo = parts[1] || '';
            const zekkenName = toZekkenDisplayName(name);
            return `${name}, ${zekkenName}, ${dojo}`;
        })
        .join('\n');
}

const sampleDataWithZekken = {
    small: toZekkenSample(sampleData.small),
    medium: toZekkenSample(sampleData.medium),
    large: toZekkenSample(sampleData.large)
};

function getSampleData(size) {
    return zekkenCheckbox.checked ? sampleDataWithZekken[size] : sampleData[size];
}

function detectLoadedSampleSize(currentText) {
    const normalizedText = currentText.trim();
    if (!normalizedText) {
        return null;
    }

    const sampleSizes = ['small', 'medium', 'large'];
    for (const size of sampleSizes) {
        if (
            normalizedText === sampleData[size].trim() ||
            normalizedText === sampleDataWithZekken[size].trim()
        ) {
            return size;
        }
    }

    return null;
}

function syncLoadedSampleToCurrentMode() {
    const currentText = playerListTextarea.value;
    const sampleSize = detectLoadedSampleSize(currentText);
    if (!sampleSize) {
        return;
    }

    setPlayerList(getSampleData(sampleSize));
}

function setPlayerList(text) {
    document.getElementById('playerList').value = text;
    localStorage.setItem('playerList', text);
    updateLineNumbers();
    updatePlayerCount();
    validateParticipantListInput();
    calculateTimeEstimate();
}

document.getElementById('loadSmallSample').addEventListener('click', function () {
    setPlayerList(getSampleData('small'));
});

document.getElementById('loadMediumSample').addEventListener('click', function () {
    setPlayerList(getSampleData('medium'));
});

document.getElementById('loadLargeSample').addEventListener('click', function () {
    setPlayerList(getSampleData('large'));
});

// File drag & drop functionality
const dropArea = document.getElementById('drop-area');
const fileInput = document.getElementById('file-input');
zekkenCheckbox.addEventListener('change', function () {
    updateZekkenHints();
    syncLoadedSampleToCurrentMode();
    validateParticipantListInput();
});

// Open file selector when clicking drop area
dropArea.addEventListener('click', () => {
    fileInput.click();
});

// Handle file selection
fileInput.addEventListener('change', (e) => {
    const file = e.target.files[0];
    if (file) {
        readFile(file);
    }
});

// Handle drag events
['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
    dropArea.addEventListener(eventName, preventDefaults, false);
});

function preventDefaults(e) {
    e.preventDefault();
    e.stopPropagation();
}

['dragenter', 'dragover'].forEach(eventName => {
    dropArea.addEventListener(eventName, () => {
        dropArea.classList.add('dragover');
    }, false);
});

['dragleave', 'drop'].forEach(eventName => {
    dropArea.addEventListener(eventName, () => {
        dropArea.classList.remove('dragover');
    }, false);
});

dropArea.addEventListener('drop', (e) => {
    const files = e.dataTransfer.files;
    if (files.length) {
        readFile(files[0]);
    }
});

// Read CSV or TXT file contents
function readFile(file) {
    if (file.type !== "text/csv" && file.type !== "text/plain" && !file.name.endsWith('.csv') && !file.name.endsWith('.txt')) {
        const errorMsg = document.getElementById('error-message');
        errorMsg.textContent = 'Error: Please upload a CSV or text file';
        errorMsg.style.display = 'block';
        setTimeout(() => {
            errorMsg.style.display = 'none';
        }, 3000);
        return;
    }

    const reader = new FileReader();

    // Add loading indicator
    const dropArea = document.getElementById('drop-area');
    dropArea.innerHTML = `
        <div class="drop-message">
            <div class="spinner-border text-primary" role="status">
                <span class="visually-hidden">Loading...</span>
            </div>
            <p class="mt-2">Reading file...</p>
        </div>
    `;

    reader.onload = function (e) {
        setPlayerList(e.target.result);

        // Success feedback
        playerListTextarea.classList.add('border-success');

        // Restore drop area
        dropArea.innerHTML = `
            <div class="drop-message">
                <i class="bi bi-check-circle-fill text-success fs-3"></i>
                <p>File loaded successfully!<br>Drag & drop another file or click to select</p>
                <input type="file" id="file-input" accept=".csv,.txt" class="d-none">
            </div>
        `;

        // Reattach event listener to the new file input
        document.getElementById('file-input').addEventListener('change', (e) => {
            const file = e.target.files[0];
            if (file) {
                readFile(file);
            }
        });

        setTimeout(() => {
            playerListTextarea.classList.remove('border-success');

            // Restore original drop area after success message
            dropArea.innerHTML = `
                <div class="drop-message">
                    <i class="bi bi-cloud-arrow-up fs-3"></i>
                    <p>Drag and drop a CSV file here <br>or click to select a file</p>
                    <input type="file" id="file-input" accept=".csv,.txt" class="d-none">
                </div>
            `;

            // Reattach event listener to the new file input
            document.getElementById('file-input').addEventListener('change', (e) => {
                const file = e.target.files[0];
                if (file) {
                    readFile(file);
                }
            });
        }, 2000);
    };

    reader.onerror = function () {
        dropArea.innerHTML = `
            <div class="drop-message">
                <i class="bi bi-exclamation-triangle-fill text-danger fs-3"></i>
                <p>Error reading file!<br>Please try again</p>
                <input type="file" id="file-input" accept=".csv,.txt" class="d-none">
            </div>
        `;

        // Reattach event listener to the new file input
        document.getElementById('file-input').addEventListener('change', (e) => {
            const file = e.target.files[0];
            if (file) {
                readFile(file);
            }
        });
    };

    reader.readAsText(file);
}

// Update player count badge and calculate statistics
function updatePlayerCount() {
    const playerList = document.getElementById('playerList').value.trim();
    const lines = playerList.split('\n').filter(line => line.trim() !== '');
    const count = lines.length;

    document.getElementById('playerCount').textContent = count + (count === 1 ? ' player' : ' players');

    return lines;
}

// Calculate and display statistics
function calculateStatistics() {
    const lines = updatePlayerCount();
    const totalPlayers = lines.length;
    document.getElementById('modalPlayerCount').textContent = totalPlayers;
    const withZekkenName = document.getElementById('withZekkenName').checked;

    // Count unique dojos
    const dojos = new Set();
    lines.forEach(line => {
        const parts = line.split(',').map(part => part.trim());
        const dojoIndex = withZekkenName ? 2 : 1;
        if (parts.length > dojoIndex) {
            const dojo = parts[dojoIndex].trim();
            if (dojo) {
                dojos.add(dojo);
            }
        }
    });
    document.getElementById('dojoCount').textContent = dojos.size;

    // Calculate pool statistics
    const isTournamentPools = document.getElementById('pools').checked;
    const poolStatsSection = document.getElementById('poolStatsSection');

    if (isTournamentPools) {
        poolStatsSection.style.display = 'block';

        const playersPerPool = parseInt(document.getElementById('playersPerPool').value) || 3;
        const winnersPerPool = parseInt(document.getElementById('winnersPerPool').value) || 2;
        const isMaxMode = document.getElementById('poolSizeMax').checked;

        // Calculate number of pools needed
        const numPools = isMaxMode ? Math.ceil(totalPlayers / playersPerPool) : Math.floor(totalPlayers / playersPerPool);
        const playoffParticipants = numPools * winnersPerPool;

        document.getElementById('poolCount').textContent = numPools;
        document.getElementById('statsPlayersPerPool').textContent = playersPerPool + (isMaxMode ? ' (Max)' : ' (Min)');
        document.getElementById('statsWinnersPerPool').textContent = winnersPerPool;
        document.getElementById('playoffParticipants').textContent = playoffParticipants;
    } else {
        poolStatsSection.style.display = 'none';
    }

    // Show the modal
    const statsModal = new bootstrap.Modal(document.getElementById('statsModal'));
    statsModal.show();
}

// Setup event listeners for statistics
document.getElementById('showStatistics').addEventListener('click', calculateStatistics);
document.getElementById('playerList').addEventListener('input', function () {
    updateLineNumbers();
    updatePlayerCount();
    scheduleParticipantValidation();
});
document.getElementById('playerList').addEventListener('scroll', function () {
    playerListLineNumbers.scrollTop = playerListTextarea.scrollTop;
});
playerListValidation.addEventListener('click', function (event) {
    const jumpButton = event.target.closest('.validation-line-jump');
    if (!jumpButton) {
        return;
    }

    const lineNumber = parseInt(jumpButton.getAttribute('data-line-number'), 10);
    if (!Number.isNaN(lineNumber) && lineNumber > 0) {
        jumpToParticipantLine(lineNumber);
    }
});
document.getElementById('playersPerPool').addEventListener('input', updatePlayerCount);
document.getElementById('winnersPerPool').addEventListener('input', updatePlayerCount);

// Call version fetch and initialize player count on page load
document.addEventListener('DOMContentLoaded', function () {
    fetchAppVersion();
    updateLineNumbers();
    updatePlayerCount();
    validateParticipantListInput();
});

// Seeding Logic
let currentSeedAssignments = [];

document.getElementById('manageSeeds').addEventListener('click', async function () {
    const playerList = document.getElementById('playerList').value.trim();
    const withZekkenName = document.getElementById('withZekkenName').checked;
    if (!playerList) {
        alert('Please enter players first.');
        return;
    }

    const participantValidation = validateParticipantListInput();
    if (participantValidation.errors.length > 0) {
        alert(`Please fix the participant list before assigning seeds.\n\n${participantValidation.errors[0]}`);
        return;
    }

    try {
        const result = await parseParticipants(playerList, withZekkenName);
        if (result.ok) {
            const data = result.data;
            const tableBody = document.getElementById('seedsTableBody');
            tableBody.innerHTML = '';
            seedsDisplayNameHeader.style.display = withZekkenName ? '' : 'none';

            // Map existing seeds if any
            const seedMap = {};
            currentSeedAssignments.forEach(a => {
                seedMap[a.Name + '|' + (a.Dojo || '')] = a.SeedRank;
            });

            data.participants.forEach((p, index) => {
                const tr = document.createElement('tr');
                const seedValue = seedMap[p.name + '|' + (p.dojo || '')] || '';
                const displayNameCell = withZekkenName
                    ? `<td>${escapeHtml(p.displayName || '')}</td>`
                    : '';
                tr.innerHTML = `
                    <td>${escapeHtml(p.name)}</td>
                    ${displayNameCell}
                    <td>${escapeHtml(p.dojo)}</td>
                    <td>
                        <input type="number" class="form-control form-control-sm seed-input"
                               data-name="${escapeHtml(p.name)}" data-dojo="${escapeHtml(p.dojo || '')}" min="1" value="${seedValue}" placeholder="Rank">
                    </td>
                `;
                tableBody.appendChild(tr);
            });

            const seedsModal = new bootstrap.Modal(document.getElementById('seedsModal'));
            seedsModal.show();
        } else {
            showToast(result.error, 'danger');
        }
    } catch (error) {
        console.error('Error fetching participants:', error);
        showToast('Could not reach the server. Check your connection and try again.', 'danger');
    }
});

document.getElementById('saveSeedsBtn').addEventListener('click', function () {
    const inputs = document.querySelectorAll('.seed-input');
    const rawInputs = [];

    inputs.forEach(input => {
        rawInputs.push({
            name: input.getAttribute('data-name'),
            dojo: input.getAttribute('data-dojo') || '',
            rawValue: input.value
        });
        // Mirror the legacy behaviour of clearing invalid non-positive integers
        // so the textbox visually shows the rejection.
        const rank = parseInt(input.value, 10);
        if (!(rank > 0) && input.value !== '') {
            input.value = '';
        }
    });

    const result = validateSeedAssignments(rawInputs);
    if (!result.ok) {
        alert(`Cannot save seeds due to the following errors:\n\n- ${result.errors.join('\n- ')}`);
        return;
    }

    currentSeedAssignments = result.assignments;
    document.getElementById('seedsInput').value = JSON.stringify(result.assignments);

    bootstrap.Modal.getInstance(document.getElementById('seedsModal')).hide();

    // Highlight the seed button if seeds are assigned
    const btn = document.getElementById('manageSeeds');
    if (result.assignments.length > 0) {
        btn.classList.add('btn-warning');
        btn.classList.remove('btn-outline-warning');
        btn.innerHTML = `<i class="bi bi-star-fill"></i> ${result.assignments.length} Seeds Assigned`;
    } else {
        btn.classList.remove('btn-warning');
        btn.classList.add('btn-outline-warning');
        btn.innerHTML = `<i class="bi bi-star"></i> Seed Participants`;
    }
});

// --- Toast notification system ---
function showToast(message, type = 'danger') {
    const container = document.getElementById('toastContainer');
    const icons = {
        danger: 'bi-exclamation-triangle-fill',
        warning: 'bi-exclamation-circle-fill',
        success: 'bi-check-circle-fill',
        info: 'bi-info-circle-fill'
    };
    const icon = icons[type] || icons.info;
    const toastEl = document.createElement('div');
    toastEl.className = `toast align-items-center text-bg-${type} border-0`;
    toastEl.setAttribute('role', 'alert');
    toastEl.innerHTML = `
        <div class="d-flex">
            <div class="toast-body"><i class="bi ${icon} me-2"></i>${escapeHtml(message)}</div>
            <button type="button" class="btn-close btn-close-white me-2 m-auto" data-bs-dismiss="toast"></button>
        </div>`;
    container.appendChild(toastEl);
    const toast = new bootstrap.Toast(toastEl, { delay: 6000 });
    toast.show();
    toastEl.addEventListener('hidden.bs.toast', () => toastEl.remove());
}

// --- Time Estimator ---
function calculateTimeEstimate() {
    const playerList = document.getElementById('playerList').value.trim();
    const lines = playerList.split('\n').filter(line => line.trim() !== '');
    const totalPlayers = lines.length;

    const emptyResult = () => {
        document.getElementById('estPoolTimeResult').textContent = '--';
        document.getElementById('estElimTimeResult').textContent = '--';
        document.getElementById('estTotalTimeResult').textContent = '--';
        document.getElementById('estElapsedTimeResult').textContent = '--';
        document.getElementById('estFinishTimeResult').textContent = '--';
    };

    const isPools = document.getElementById('pools').checked;
    const startTimeStr = document.getElementById('estStartTime').value || '09:00';

    const estimate = estimateSchedule({
        totalPlayers,
        isPools,
        courts: document.getElementById('courts').value,
        teamSize: document.getElementById('teamMatches').value,
        poolMatchMins: document.getElementById('estPoolMatchTime').value,
        elimMatchMins: document.getElementById('estElimMatchTime').value,
        rotationSecs: document.getElementById('estRotationPadding').value,
        breakMins: document.getElementById('estBreakTime').value,
        playersPerPool: document.getElementById('playersPerPool').value,
        winnersPerPool: document.getElementById('winnersPerPool').value,
        isMaxMode: document.getElementById('poolSizeMax').checked,
        isRoundRobin: document.getElementById('roundRobin').checked,
        startTimeMinutes: parseStartTime(startTimeStr)
    });

    if (estimate === null) {
        emptyResult();
        return;
    }

    document.getElementById('estPoolTimeResult').textContent = isPools ? formatDuration(estimate.totalPoolMinutes) : '--';
    document.getElementById('estElimTimeResult').textContent = formatDuration(estimate.totalElimMinutes);
    document.getElementById('estTotalTimeResult').textContent = formatDuration(estimate.totalParallelMinutes);
    document.getElementById('estCourtsLabel').textContent = estimate.courts;
    // The elapsed estimate comes from the server. Reset to a placeholder
    // until the fetch resolves so the user never sees a stale prior value
    // briefly attached to fresh inputs.
    document.getElementById('estElapsedTimeResult').textContent = '…';
    document.getElementById('estFinishTimeResult').textContent = formatTime(estimate.finishTotalMins);

    document.getElementById('estPoolTimeCol').style.display = isPools ? '' : 'none';
    document.getElementById('estPoolMatchTimeGroup').style.display = isPools ? '' : 'none';

    // FR-059 / T152: fetch the canonical elapsed-time estimate from the
    // server. The displayed Clock time (above) is the local rich-breakdown
    // estimate; the Elapsed estimate (below) is clock × multiplier ×
    // slowest-court buffer. The two are intentionally distinct numbers.
    refreshServerEstimate(estimate).catch(() => {});
}

// refreshServerEstimate fetches the canonical elapsed-time estimate from
// GET /api/schedule/estimate, splitting Pools+Playoffs into one fetch per
// phase so each phase's matchDuration is respected. Renders the result to
// #estElapsedTimeResult and recomputes #estFinishTimeResult against it.
// Silent on failure: the placeholder stays so the user sees that the
// server estimate is unavailable rather than a stale value.
async function refreshServerEstimate(local) {
    const courts = parseInt(document.getElementById('courts').value, 10) || 1;
    const poolDur = parseFloat(document.getElementById('estPoolMatchTime').value) || 3;
    const elimDur = parseFloat(document.getElementById('estElimMatchTime').value) || 4;
    const teamSize = Math.max(local.teamSize || 1, 1);

    // multiplier 1.5 = clock→elapsed conversion; buffer 10 = slowest-court
    // padding % (typical 10–15). Both mirror the defaults in
    // internal/engine/schedule.go.
    const baseParams = { multiplier: 1.5, courts, teamSize: 0, buffer: 10 };

    const calls = [];
    if (local.isPools && local.numPoolMatches > 0) {
        calls.push(fetchScheduleEstimate({
            ...baseParams,
            matchDuration: poolDur,
            numMatches: local.numPoolMatches * teamSize
        }));
    }
    if (local.numElimMatches > 0) {
        calls.push(fetchScheduleEstimate({
            ...baseParams,
            matchDuration: elimDur,
            numMatches: local.numElimMatches * teamSize
        }));
    }
    if (calls.length === 0) {
        document.getElementById('estElapsedTimeResult').textContent = '--';
        return;
    }

    const results = await Promise.all(calls);
    if (results.some(r => !r || typeof r.totalDurationMinutes !== 'number')) {
        document.getElementById('estElapsedTimeResult').textContent = '--';
        return;
    }
    const totalMins = results.reduce((s, r) => s + r.totalDurationMinutes, 0);
    document.getElementById('estElapsedTimeResult').textContent = formatDuration(totalMins);
    const startMins = parseStartTime(document.getElementById('estStartTime').value || '09:00');
    document.getElementById('estFinishTimeResult').textContent = formatTime(startMins + totalMins);
}

// Wire up estimator to all relevant inputs
const estimatorInputIds = [
    'estPoolMatchTime', 'estElimMatchTime', 'estRotationPadding',
    'estBreakTime', 'estStartTime', 'courts', 'teamMatches',
    'playersPerPool', 'winnersPerPool'
];
estimatorInputIds.forEach(id => {
    document.getElementById(id).addEventListener('input', calculateTimeEstimate);
});
['playoffs', 'pools', 'roundRobin', 'poolSizeMin', 'poolSizeMax'].forEach(id => {
    document.getElementById(id).addEventListener('change', calculateTimeEstimate);
});
document.getElementById('playerList').addEventListener('input', calculateTimeEstimate);

// Persist estimator values to localStorage
const estimatorPersistIds = [
    'estPoolMatchTime', 'estElimMatchTime', 'estRotationPadding',
    'estBreakTime', 'estStartTime'
];
estimatorPersistIds.forEach(id => {
    const el = document.getElementById(id);
    el.addEventListener('input', () => localStorage.setItem(id, el.value));
});

// Restore estimator values from localStorage on load
window.addEventListener('DOMContentLoaded', () => {
    estimatorPersistIds.forEach(id => {
        const saved = localStorage.getItem(id);
        if (saved !== null) {
            document.getElementById(id).value = saved;
        }
    });
    calculateTimeEstimate();
});
