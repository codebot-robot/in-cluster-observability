// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package debugendpoint

const ExplorerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Ollie Trace Explorer</title>
    <style>
        :root {
            --bg-main: #0b0f19;
            --bg-panel: #111827;
            --bg-hover: #1f2937;
            --border-color: #374151;
            --text-primary: #f3f4f6;
            --text-secondary: #9ca3af;
            --color-accent: #3b82f6;
            --color-success: #10b981;
            --color-warning: #f59e0b;
            --color-error: #ef4444;
            --color-purple: #8b5cf6;
        }

        body {
            margin: 0;
            padding: 0;
            background-color: var(--bg-main);
            color: var(--text-primary);
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            display: flex;
            flex-direction: column;
            height: 100vh;
            overflow: hidden;
        }

        header {
            background-color: var(--bg-panel);
            border-bottom: 1px solid var(--border-color);
            padding: 12px 24px;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }

        header h1 {
            margin: 0;
            font-size: 18px;
            font-weight: 600;
            display: flex;
            align-items: center;
            gap: 10px;
        }

        header h1 span.logo {
            font-weight: 800;
            letter-spacing: 0.5px;
            background: linear-gradient(135deg, #60a5fa, #a78bfa);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        header h1 span.badge {
            background-color: rgba(59, 130, 246, 0.15);
            color: var(--color-accent);
            border: 1px solid rgba(59, 130, 246, 0.3);
            padding: 2px 8px;
            font-size: 11px;
            border-radius: 4px;
            text-transform: uppercase;
            font-family: monospace;
        }

        .main-container {
            display: flex;
            flex: 1;
            overflow: hidden;
        }

        .sidebar {
            width: 380px;
            background-color: var(--bg-panel);
            border-right: 1px solid var(--border-color);
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }

        .search-box {
            padding: 16px;
            border-bottom: 1px solid var(--border-color);
            display: flex;
            gap: 8px;
        }

        .search-box input {
            flex: 1;
            background-color: var(--bg-main);
            border: 1px solid var(--border-color);
            border-radius: 6px;
            padding: 8px 12px;
            color: var(--text-primary);
            font-size: 14px;
            outline: none;
        }

        .search-box input:focus {
            border-color: var(--color-accent);
            box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
        }

        .btn {
            background-color: var(--color-accent);
            color: white;
            border: none;
            border-radius: 6px;
            padding: 8px 14px;
            cursor: pointer;
            font-weight: 500;
            font-size: 14px;
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 6px;
            transition: background-color 0.15s;
        }

        .btn:hover {
            background-color: #2563eb;
        }

        .trace-list {
            flex: 1;
            overflow-y: auto;
        }

        .trace-item {
            padding: 14px 16px;
            border-bottom: 1px solid var(--border-color);
            cursor: pointer;
            transition: background-color 0.15s;
        }

        .trace-item:hover {
            background-color: var(--bg-hover);
        }

        .trace-item.active {
            background-color: rgba(59, 130, 246, 0.08);
            border-left: 4px solid var(--color-accent);
            padding-left: 12px;
        }

        .trace-item-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 6px;
        }

        .trace-name {
            font-weight: 600;
            font-size: 14px;
            color: var(--text-primary);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            max-width: 250px;
        }

        .trace-meta {
            display: flex;
            justify-content: space-between;
            font-size: 12px;
            color: var(--text-secondary);
        }

        .badge-status {
            padding: 2px 6px;
            border-radius: 4px;
            font-size: 11px;
            font-weight: 600;
        }

        .status-success {
            background-color: rgba(16, 185, 129, 0.15);
            color: var(--color-success);
        }

        .status-error {
            background-color: rgba(239, 68, 68, 0.15);
            color: var(--color-error);
        }

        .status-warn {
            background-color: rgba(245, 158, 11, 0.15);
            color: var(--color-warning);
        }

        .viewer {
            flex: 1;
            display: flex;
            flex-direction: column;
            overflow: hidden;
            background-color: var(--bg-main);
        }

        .no-selection {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            flex: 1;
            color: var(--text-secondary);
            gap: 16px;
        }

        .no-selection-icon {
            font-size: 48px;
            opacity: 0.3;
        }

        .trace-details-header {
            background-color: var(--bg-panel);
            border-bottom: 1px solid var(--border-color);
            padding: 16px 24px;
        }

        .trace-details-title-row {
            display: flex;
            justify-content: space-between;
            align-items: flex-start;
            margin-bottom: 8px;
        }

        .trace-details-title {
            margin: 0;
            font-size: 20px;
            font-weight: 700;
        }

        .trace-details-id {
            font-family: monospace;
            font-size: 12px;
            color: var(--text-secondary);
        }

        .trace-details-stats {
            display: flex;
            gap: 20px;
            font-size: 13px;
            color: var(--text-secondary);
        }

        .stat-item strong {
            color: var(--text-primary);
        }

        .waterfall-container {
            flex: 1;
            overflow-y: auto;
            padding: 20px 24px;
        }

        .waterfall-headers {
            display: flex;
            border-bottom: 1px solid var(--border-color);
            padding-bottom: 10px;
            font-size: 12px;
            font-weight: 700;
            color: var(--text-secondary);
            text-transform: uppercase;
            letter-spacing: 0.5px;
        }

        .waterfall-header-label {
            width: 40%;
        }

        .waterfall-header-bar {
            width: 60%;
            position: relative;
        }

        .waterfall-row {
            border-bottom: 1px solid rgba(55, 65, 81, 0.4);
        }

        .span-row {
            display: flex;
            align-items: center;
            padding: 12px 0;
            cursor: pointer;
            transition: background-color 0.15s;
        }

        .span-row:hover {
            background-color: var(--bg-hover);
        }

        .span-label {
            width: 40%;
            display: flex;
            align-items: center;
            font-size: 13px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            padding-right: 12px;
            box-sizing: border-box;
        }

        .span-indent {
            display: inline-block;
            height: 16px;
            border-left: 1px dashed rgba(156, 163, 175, 0.25);
            margin-right: 12px;
        }

        .span-name {
            font-weight: 600;
            color: var(--text-primary);
        }

        .span-method {
            font-size: 11px;
            font-weight: 700;
            padding: 2px 5px;
            border-radius: 3px;
            background-color: rgba(156, 163, 175, 0.15);
            color: var(--text-secondary);
            margin-right: 6px;
        }

        .span-bar-container {
            width: 60%;
            position: relative;
            height: 24px;
            display: flex;
            align-items: center;
        }

        .span-bar {
            position: absolute;
            height: 10px;
            border-radius: 3px;
            background: linear-gradient(90deg, var(--color-accent), var(--color-purple));
            min-width: 4px;
            transition: filter 0.15s;
        }

        .span-bar:hover {
            filter: brightness(1.25);
        }

        .span-bar-label {
            position: absolute;
            font-size: 11px;
            font-weight: 500;
            color: var(--text-secondary);
            margin-left: 6px;
            white-space: nowrap;
        }

        .span-details {
            display: none;
            background-color: rgba(17, 24, 39, 0.6);
            border-top: 1px solid rgba(55, 65, 81, 0.4);
            padding: 16px 24px;
            font-size: 12px;
        }

        .span-details.open {
            display: block;
        }

        .details-section-title {
            font-size: 12px;
            font-weight: 700;
            text-transform: uppercase;
            letter-spacing: 0.5px;
            color: var(--text-secondary);
            margin-bottom: 8px;
            margin-top: 12px;
        }

        .details-section-title:first-child {
            margin-top: 0;
        }

        .details-grid {
            display: grid;
            grid-template-columns: 200px 1fr;
            gap: 6px 12px;
            background-color: rgba(15, 23, 42, 0.4);
            border: 1px solid rgba(55, 65, 81, 0.3);
            border-radius: 6px;
            padding: 12px;
        }

        .details-key {
            color: var(--text-secondary);
            font-weight: 500;
            font-family: monospace;
        }

        .details-value {
            font-family: monospace;
            word-break: break-all;
            color: #34d399; /* emerald green */
        }

        .span-row-icon {
            width: 16px;
            height: 16px;
            display: flex;
            align-items: center;
            justify-content: center;
            margin-right: 6px;
            color: var(--text-secondary);
            font-family: monospace;
            font-weight: bold;
            font-size: 12px;
            user-select: none;
        }
    </style>
</head>
<body>
    <header>
        <h1><span class="logo">Ollie</span> <span class="badge">Trace Explorer</span></h1>
        <button id="refresh-btn" class="btn">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"/></svg>
            Refresh
        </button>
    </header>

    <div class="main-container">
        <div class="sidebar">
            <div class="search-box">
                <input type="text" id="search-input" placeholder="Search traces (e.g. path, service)...">
            </div>
            <div class="trace-list" id="trace-list-container">
                <!-- Trace list items will be injected here -->
            </div>
        </div>

        <div class="viewer" id="viewer-container">
            <div class="no-selection" id="no-selection-panel">
                <div class="no-selection-icon">🔍</div>
                <h3>Select a trace from the list to view its execution waterfall</h3>
            </div>

            <!-- Trace detail will be injected here -->
        </div>
    </div>

    <script>
        let currentTraces = [];
        let selectedTraceId = null;

        document.addEventListener('DOMContentLoaded', () => {
            fetchTraces();
            document.getElementById('refresh-btn').addEventListener('click', fetchTraces);
            document.getElementById('search-input').addEventListener('input', renderTraceList);
        });

        async function fetchTraces() {
            try {
                const res = await fetch('/debug/api/traces');
                if (!res.ok) throw new Error('API request failed');
                currentTraces = await res.json();
                renderTraceList();
            } catch (err) {
                console.error('Error fetching traces:', err);
                document.getElementById('trace-list-container').innerHTML = 
                    '<div style="padding: 20px; color: var(--color-error); text-align: center;">' +
                        'Failed to fetch traces: ' + err.message +
                    '</div>';
            }
        }

        function renderTraceList() {
            const container = document.getElementById('trace-list-container');
            const searchVal = document.getElementById('search-input').value.toLowerCase();

            const filtered = currentTraces.filter(t => {
                return (
                    t.trace_id.toLowerCase().includes(searchVal) ||
                    t.root_span_name.toLowerCase().includes(searchVal) ||
                    t.method.toLowerCase().includes(searchVal) ||
                    t.path.toLowerCase().includes(searchVal) ||
                    (t.service_name && t.service_name.toLowerCase().includes(searchVal))
                );
            });

            if (filtered.length === 0) {
                container.innerHTML = 
                    '<div style="padding: 24px; text-align: center; color: var(--text-secondary);">' +
                        'No traces found' +
                    '</div>';
                return;
            }

            container.innerHTML = filtered.map(t => {
                const durationStr = formatDuration(t.duration_ns);
                const statusClass = t.status_code >= 400 ? 'status-error' : (t.status_code >= 300 ? 'status-warn' : 'status-success');
                const activeClass = t.trace_id === selectedTraceId ? 'active' : '';
                const pathDisplay = t.path || t.root_span_name;

                return '<div class="trace-item ' + activeClass + '" onclick="selectTrace(\'' + t.trace_id + '\')">' +
                        '<div class="trace-item-header">' +
                            '<span class="trace-name">' + (t.method ? '<span class="span-method">' + t.method + '</span>' : '') + pathDisplay + '</span>' +
                            '<span class="badge-status ' + statusClass + '">' + (t.status_code || 'span') + '</span>' +
                        '</div>' +
                        '<div class="trace-meta">' +
                            '<span>' + (t.service_name || 'unknown-service') + ' (' + t.span_count + ' span' + (t.span_count > 1 ? 's' : '') + ')</span>' +
                            '<strong style="color: var(--text-primary);">' + durationStr + '</strong>' +
                        '</div>' +
                    '</div>';
            }).join('');
        }

        async function selectTrace(traceId) {
            selectedTraceId = traceId;
            renderTraceList();

            const viewer = document.getElementById('viewer-container');
            viewer.innerHTML = 
                '<div class="no-selection">' +
                    '<div style="text-align: center;">' +
                        '<div style="margin-bottom: 12px; font-size: 24px;">⌛</div>' +
                        '<div>Loading trace details...</div>' +
                    '</div>' +
                '</div>';

            try {
                const res = await fetch('/debug/api/traces?trace_id=' + traceId);
                if (!res.ok) throw new Error('Failed to fetch trace spans');
                const spans = await res.json();
                renderTraceWaterfall(traceId, spans);
            } catch (err) {
                console.error('Error fetching trace spans:', err);
                viewer.innerHTML = 
                    '<div class="no-selection" style="color: var(--color-error);">' +
                        '<h3>Failed to load trace waterfall</h3>' +
                        '<div>' + err.message + '</div>' +
                    '</div>';
            }
        }

        function buildSpanTree(spans) {
            const map = {};
            const roots = [];
            spans.forEach(s => {
                map[s.span_id] = { span: s, children: [] };
            });
            spans.forEach(s => {
                const parentId = s.parent_span_id;
                if (parentId && parentId !== "0000000000000000" && map[parentId]) {
                    map[parentId].children.push(map[s.span_id]);
                } else {
                    roots.push(map[s.span_id]);
                }
            });
            return roots;
        }

        function flattenSpanTree(nodes, depth = 0, result = []) {
            nodes.forEach(node => {
                node.depth = depth;
                result.push(node);
                node.children.sort((a, b) => a.span.start_time_ns - b.span.start_time_ns);
                flattenSpanTree(node.children, depth + 1, result);
            });
            return result;
        }

        function renderTraceWaterfall(traceId, spans) {
            const viewer = document.getElementById('viewer-container');
            if (spans.length === 0) {
                viewer.innerHTML = 
                    '<div class="no-selection">' +
                        '<h3>No spans found for this trace ID</h3>' +
                    '</div>';
                return;
            }

            // Calculate min start and max end
            let traceStart = spans[0].start_time_ns;
            let traceEnd = spans[0].end_time_ns;

            spans.forEach(s => {
                if (s.start_time_ns < traceStart) traceStart = s.start_time_ns;
                if (s.end_time_ns > traceEnd) traceEnd = s.end_time_ns;
            });

            const traceDuration = traceEnd - traceStart || 1;
            const treeRoots = buildSpanTree(spans);
            const flatSpans = flattenSpanTree(treeRoots);

            // Find overall service name or info
            const earliestSpan = spans.sort((a, b) => a.start_time_ns - b.start_time_ns)[0];
            const serviceName = earliestSpan.attributes['service.name'] || earliestSpan.attributes['k8s.pod.name'] || 'unknown-service';

            let html = '<div class="trace-details-header">' +
                '<div class="trace-details-title-row">' +
                    '<h2 class="trace-details-title">' + (earliestSpan.method ? earliestSpan.method + ' ' : '') + escapeHTML(earliestSpan.path || earliestSpan.name) + '</h2>' +
                    '<span class="badge-status ' + (earliestSpan.status_code >= 400 ? 'status-error' : 'status-success') + '">' + (earliestSpan.status_code || 'OK') + '</span>' +
                '</div>' +
                '<div class="trace-details-id">Trace ID: ' + traceId + '</div>' +
                '<div class="trace-details-stats" style="margin-top: 12px;">' +
                    '<div class="stat-item">Service: <strong>' + serviceName + '</strong></div>' +
                    '<div class="stat-item">Duration: <strong>' + formatDuration(traceDuration) + '</strong></div>' +
                    '<div class="stat-item">Spans: <strong>' + spans.length + '</strong></div>' +
                '</div>' +
            '</div>' +
            '<div class="waterfall-container">' +
                '<div class="waterfall-headers">' +
                    '<div class="waterfall-header-label">Spans</div>' +
                    '<div class="waterfall-header-bar">Waterfall</div>' +
                '</div>';

            flatSpans.forEach((node, index) => {
                const s = node.span;
                const durationNs = s.end_time_ns - s.start_time_ns || 0;
                const leftPct = ((s.start_time_ns - traceStart) / traceDuration) * 100;
                const widthPct = Math.max(0.5, (durationNs / traceDuration) * 100);

                let indents = '';
                for (let i = 0; i < node.depth; i++) {
                    indents += '<span class="span-indent"></span>';
                }

                // Prepare attribute rows
                let attrRows = '';
                for (const [k, v] of Object.entries(s.attributes || {})) {
                    attrRows += '<span class="details-key">' + escapeHTML(k) + '</span>' +
                                '<span class="details-value">' + escapeHTML(v) + '</span>';
                }

                html += '<div class="waterfall-row">' +
                            '<div class="span-row" onclick="toggleSpanDetails(\'' + traceId + '-' + index + '\')">' +
                                '<div class="span-label">' +
                                    indents +
                                    '<div class="span-row-icon" id="icon-' + traceId + '-' + index + '">▸</div>' +
                                    (s.method ? '<span class="span-method">' + s.method + '</span>' : '') +
                                    '<span class="span-name" title="' + escapeHTML(s.name) + '">' + escapeHTML(s.name) + '</span>' +
                                '</div>' +
                                '<div class="span-bar-container">' +
                                    '<div class="span-bar" style="left: ' + leftPct + '%; width: ' + widthPct + '%;"></div>' +
                                    '<span class="span-bar-label" style="left: ' + (leftPct + widthPct) + '%;">' + formatDuration(durationNs) + '</span>' +
                                '</div>' +
                            '</div>' +
                            '<div class="span-details" id="details-' + traceId + '-' + index + '">' +
                                '<div class="details-section-title">Span Properties</div>' +
                                '<div class="details-grid" style="margin-bottom: 12px;">' +
                                    '<span class="details-key">Name</span><span class="details-value" style="color: var(--text-primary);">' + escapeHTML(s.name) + '</span>' +
                                    '<span class="details-key">Span ID</span><span class="details-value" style="color: var(--text-primary);">' + s.span_id + '</span>' +
                                    '<span class="details-key">Parent Span ID</span><span class="details-value" style="color: var(--text-primary);">' + (s.parent_span_id || 'None (Root)') + '</span>' +
                                    '<span class="details-key">Duration</span><span class="details-value" style="color: var(--text-primary);">' + formatDuration(durationNs) + ' (' + durationNs.toLocaleString() + ' ns)</span>' +
                                '</div>';

                if (attrRows) {
                    html += '<div class="details-section-title">Attributes</div>' +
                            '<div class="details-grid">' + attrRows + '</div>';
                }

                html += '</div></div>';
            });

            html += '</div>';
            viewer.innerHTML = html;
        }

        function toggleSpanDetails(id) {
            const panel = document.getElementById('details-' + id);
            const icon = document.getElementById('icon-' + id);
            if (panel && icon) {
                const isOpen = panel.classList.contains('open');
                if (isOpen) {
                    panel.classList.remove('open');
                    icon.textContent = '▸';
                } else {
                    panel.classList.add('open');
                    icon.textContent = '▾';
                }
            }
        }

        function formatDuration(ns) {
            if (ns >= 1e9) return (ns / 1e9).toFixed(2) + "s";
            if (ns >= 1e6) return (ns / 1e6).toFixed(2) + "ms";
            if (ns >= 1e3) return (ns / 1e3).toFixed(2) + "µs";
            return ns + "ns";
        }

        function escapeHTML(str) {
            if (!str) return '';
            return str
                .replace(/&/g, '&amp;')
                .replace(/</g, '&lt;')
                .replace(/>/g, '&gt;')
                .replace(/"/g, '&quot;')
                .replace(/'/g, '&#039;');
        }
    </script>
</body>
</html>`
