(function () {

    const TICK_GAP_PX = 1;
    const TICK_WIDTH_PX = 10;
    const CHART_PADDING = 20;
    const PAGE_SIZE = 10;
    const ZOOM_PERCENT = 20;

    let TICKS = [];
    let ENTRIES = [];
    let FILTER_ID = 0;
    let HAS_MORE_AFTER = true;
    let HAS_MORE_BEFORE = true;
    let IS_LOADING_AFTER = false;
    let IS_LOADING_BEFORE = false;

    let EPOCH_MIN, EPOCH_MAX; // min and max epoch from ticks
    let EPOCH_START, EPOCH_END; // min and max epoch form input
    let EPOCH_DESIRED, EPOCH_CURRENT, NUM_TICKS, INTERVAL;

    let INTERVAL_UNIT = ''; // s=seconds, m=minutes, h=hours, d=days
    let INTERVAL_STR = '';

    let momentStart = moment().startOf('hour');
    let momentEnd = moment();
    let expression = '';
    let levels = new Set(['debug', 'info', 'warn', 'error']);

    let $bars;
    let $count;
    let $chart;
    let $container;
    let $content;
    let $needle;

    $(function () {
        $chart = $("#chart");
        $bars = $(".bars", $chart);
        $count = $('> .count', $chart);
        $needle = $(".needle", $chart);
        $container = $("#tab-content");
        $content = $("> table", $container);

        let $exp = $('#expression')
        $exp.keyup(debounce(() => {
            let newExp = $exp.val().trim();
            if (newExp != expression) {
                expression = $exp.val();
                clearEntries(true);
                updateTick();
            }
        }, 350));

        levels.forEach((level) => {
            let $checkbox = $('#check-' + level);
            $checkbox.change(() => {
                if ($checkbox.is(':checked')) {
                    levels.add(level);
                } else {
                    levels.delete(level);
                }
                clearEntries(true);
                updateTick();
            })
        });

        (function () {
            let left;
            let tick;
            $needle.draggable({
                axis: "x",
                containment: "parent",
                handle: ".handle",
                revertDuration: 200,
                revert: function () {
                    tick = TICKS[TICKS.length - 1];
                    for (let index = 0; index < TICKS.length; index++) {
                        const nextTick = TICKS[index];
                        if (nextTick.Left - CHART_PADDING > left) {
                            tick = TICKS[index - 1]
                            break
                        }
                    }
                    return (!tick || tick.Count == 0)
                },
                drag: function (event, ui) {
                    left = ui.position.left;
                },
                stop: function (event, ui) {
                    if (!tick || tick.Count == 0) {
                        return;
                    }

                    EPOCH_DESIRED = tick.EpochStart + Math.ceil((tick.EpochEnd - tick.EpochStart) / 2);
                    clearEntries();
                    loadEntries('after');
                    loadEntries('before');
                    EPOCH_DESIRED = null;
                }
            });
        })()

        const onUpdateRange = function (newStart, newEnd) {
            momentEnd = newEnd;
            momentStart = newStart;

            EPOCH_END = momentEnd.unix();
            EPOCH_START = momentStart.unix();

            // temporary
            EPOCH_MIN = EPOCH_START;
            EPOCH_MAX = EPOCH_END;

            $('#date-range strong').html(momentStart.format('YYYY-MM-DD HH:mm') + ' - ' + momentEnd.format('YYYY-MM-DD HH:mm'));
            clearEntries();
            updateTick();
        }

        $('#date-range').daterangepicker({
            endDate: momentEnd,
            startDate: momentStart,
            timePicker: true,
            timePicker24Hour: true,
            opens: "right",
            showDropdowns: true,
            ranges: {
                'Past 5 Minutes': [moment().subtract(5, 'minutes'), moment()],
                'Past 15 Minutes': [moment().subtract(15, 'minutes'), moment()],
                'Past 30 Minutes': [moment().subtract(30, 'minutes'), moment()],
                'Past 1 Hour': [moment().subtract(1, 'hours'), moment()],
                'Past 4 Hours': [moment().subtract(4, 'hours'), moment()],
                'Today': [moment().startOf('day'), moment()],
                'Yesterday': [moment().subtract(1, 'days').startOf('day'), moment().subtract(1, 'days').endOf('day')],
                'Past 2 Days': [moment().subtract(2, 'days').startOf('day'), moment().subtract(2, 'days').endOf('day')],
                'Last 7 Days': [moment().subtract(6, 'days').startOf('day'), moment()],
                'Last 30 Days': [moment().subtract(29, 'days').startOf('day'), moment()],
                'This Month': [moment().startOf('month'), moment().endOf('month')],
                'Last Month': [moment().subtract(1, 'month').startOf('month'), moment().subtract(1, 'month').endOf('month')]
            },
            locale: {
                format: 'DD/M hh:mm A'
            }
        }, function (newStart, newEnd, label) {
            switch (label) {
                case 'Custom Range': break
                case 'Past 5 Minutes': [newStart, newEnd] = [moment().subtract(5, 'minutes'), moment()]; break
                case 'Past 15 Minutes': [newStart, newEnd] = [moment().subtract(15, 'minutes'), moment()]; break
                case 'Past 30 Minutes': [newStart, newEnd] = [moment().subtract(30, 'minutes'), moment()]; break
                case 'Past 1 Hour': [newStart, newEnd] = [moment().subtract(1, 'hours'), moment()]; break
                case 'Past 4 Hours': [newStart, newEnd] = [moment().subtract(4, 'hours'), moment()]; break
                case 'Today': [newStart, newEnd] = [moment().startOf('day'), moment()]; break
                case 'Yesterday': [newStart, newEnd] = [moment().subtract(1, 'days').startOf('day'), moment().subtract(1, 'days').endOf('day')]; break
                case 'Past 2 Days': [newStart, newEnd] = [moment().subtract(2, 'days').startOf('day'), moment().subtract(2, 'days').endOf('day')]; break
                case 'Last 7 Days': [newStart, newEnd] = [moment().subtract(6, 'days').startOf('day'), moment()]; break
                case 'Last 30 Days': [newStart, newEnd] = [moment().subtract(29, 'days').startOf('day'), moment()]; break
                case 'This Month': [newStart, newEnd] = [moment().startOf('month'), moment().endOf('month')]; break
                case 'Last Month': [newStart, newEnd] = [moment().subtract(1, 'month').startOf('month'), moment().subtract(1, 'month').endOf('month')]; break
            }
            onUpdateRange(newStart, newEnd);
        });
        onUpdateRange(momentStart, momentEnd);

        $container.on("scroll", debounce(checkScroll, 20));

        window.addEventListener('resize', debounce(() => {
            onUpdateRange(momentStart, momentEnd);
        }));

        $('.zoom-in', $chart).click(() => {
            let seconds = Math.ceil((EPOCH_END - EPOCH_START) * ZOOM_PERCENT / 100);
            if (seconds < 1) {
                return
            }

            let newStart = momentStart.clone().add(seconds, 'seconds');
            let newEnd = momentEnd.clone().subtract(seconds, 'seconds');

            if (newStart.isSameOrAfter(newEnd)) {
                return;
            }

            onUpdateRange(newStart, newEnd);
        });

        $('.zoom-out', $chart).click(() => {
            let seconds = Math.ceil((EPOCH_END - EPOCH_START) * ZOOM_PERCENT / 100);
            if (seconds < 1) {
                return
            }

            let newStart = momentStart.clone().subtract(seconds, 'seconds');
            let newEnd = momentEnd.clone().add(seconds, 'seconds');

            onUpdateRange(newStart, newEnd);
        });



    });

    function setCurrentVisibleEntry(entry) {
        let tick = entry.Tick;
        if (!tick) {
            EPOCH_CURRENT = null;
            return
        }

        EPOCH_CURRENT = entry.Epoch

        let percent = Math.min(Math.max(((entry.Epoch - tick.EpochStart) / (tick.EpochEnd - tick.EpochStart)), 0), 1)
        $needle.css("left", (tick.Left - 20 + (percent * TICK_WIDTH_PX)) + "px");
    }

    function checkScroll() {
        let scrollTop = $container.scrollTop();
        let contentOffset = $content.offset().top;
        let contentHeight = $content.height();
        let containerHeight = $container.height();

        if (scrollTop < 250) {
            loadEntries('after');
        }

        if ((contentHeight - (containerHeight + scrollTop) < 250)) {
            loadEntries('before');
        }

        if (ENTRIES.length > 0) {
            // first visible element
            for (let index = 0; index < ENTRIES.length; index++) {
                const entry = ENTRIES[index];
                const offset = $(entry.Element).offset().top - contentOffset;
                if (offset > scrollTop) {
                    setCurrentVisibleEntry(entry);
                    break;
                }
            }
        }
    }

    function clearEntries(force) {
        FILTER_ID++;
        HAS_MORE_AFTER = true;
        HAS_MORE_BEFORE = true;
        IS_LOADING_AFTER = false;
        IS_LOADING_BEFORE = false;
        $count.text('');

        if (EPOCH_DESIRED || force) {
            ENTRIES = [];
            $("> tbody tr", $content).remove();
        } else {
            // only remove records that are not within the range [EPOCH_START, EPOCH_END]
            // used for zooming and when the user selects another period
            // prevents clearing all content, keeping the current scroll
            ENTRIES = ENTRIES.filter(entry => {
                if (entry.Epoch < EPOCH_START || entry.Epoch > EPOCH_END) {
                    entry.Element.remove();
                    return false
                }
                return true;
            });
        }
    }

    function calcCurrInterval(seconds) {
        // let seconds = (ms / 1000).toFixed(1);
        let minutes = (seconds / (60)).toFixed(1);
        let hours = (seconds / (60 * 60)).toFixed(1);
        let days = (seconds / (60 * 60 * 24)).toFixed(1);
        if (seconds < 60) {
            INTERVAL_UNIT = "s";
            INTERVAL_STR = Math.ceil(seconds) + "s"
        } else if (minutes < 60) {
            INTERVAL_UNIT = "m";
            INTERVAL_STR = Math.ceil(minutes) + "m";
        } else if (hours < 24) {
            INTERVAL_UNIT = "h";
            INTERVAL_STR = Math.ceil(hours) + "h";
        } else {
            INTERVAL_UNIT = "d";
            INTERVAL_STR = Math.ceil(days) + "d"
        }
    }

    function updateTick() {

        const width = $bars[0].clientWidth - (CHART_PADDING * 2);
        NUM_TICKS = Math.floor(width / (TICK_WIDTH_PX + TICK_GAP_PX));
        INTERVAL = Math.ceil((EPOCH_END - EPOCH_START) / NUM_TICKS); // every tick duration

        calcCurrInterval(INTERVAL);

        TICKS = [...Array(NUM_TICKS)].map((v, i) => {
            return {
                // Date: new Date((EPOCH_START*1000) + i * INTERVAL*1000 + DATE_OFFSET),
                Date: momentStart.clone().add(INTERVAL * i, 'seconds'),
                Left: CHART_PADDING + i * (TICK_WIDTH_PX + TICK_GAP_PX),
                EpochStart: 0,
                EpochEnd: 0,
                Count: 0,
                Debug: 0,
                Info: 0,
                Warn: 0,
                Error: 0,
                Entries: []
            }
        });

        requestTicks();
    }

    /**
      * Retrieves the markers for the filter and the defined time range.
      * 
      * With this information, it is possible to paginate the results (Keyset Pagination).
      */
    function requestTicks() {

        const $chart = document.querySelector("#chart");
        const $bars = $chart.querySelector(".bars");
        $bars.innerHTML = '';

        let params = {
            "expr": expression,
            "epoch": EPOCH_END,
            "interval": INTERVAL,
            "limit": NUM_TICKS,
        };
        if (levels.size != 4) {
            params["level"] = Array.prototype.join.call(levels.values().toArray());
        }

        const url = "./api/ticks?" + new URLSearchParams(params).toString();

        let filterId = FILTER_ID;

        fetch(url)
            .then(data => data.json())
            .then((result) => {
                if (filterId != FILTER_ID) {
                    return
                }

                if (!result.ticks || result.ticks.length == 0) {
                    // @TODO: ASYNC
                    // entries: null, scheduled: false, tasks: null
                    HAS_MORE_AFTER = false;
                    HAS_MORE_BEFORE = false;
                    return;
                }

                let max = Number.MIN_SAFE_INTEGER;
                let count = 0;

                result.ticks.forEach(it => {
                    let tick = TICKS[it.index];
                    tick.Count = it.count;
                    tick.Debug = it.debug;
                    tick.Info = it.info;
                    tick.Warn = it.warn;
                    tick.Error = it.error;
                    tick.EpochEnd = it.epoch_end;
                    tick.EpochStart = it.epoch_start;
                    tick.Date = moment(new Date(tick.EpochStart * 1000));
                    count += tick.Count;
                    max = Math.max(max, tick.Count);
                });

                // https://stackoverflow.com/a/2901298/2204014
                $('> .count', $chart).text((count).toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",") + ' events');

                // artificial padding
                max = Math.floor(max * 1.2);

                // horizontal line
                const $hr = document.createElement('div');
                $hr.classList.add('hr');
                $hr.textContent = `${Math.round(max)}`;
                $bars.appendChild($hr);

                // EPOCH_START_VALUE, EPOCH_END_VALUE,
                EPOCH_MIN = Number.MAX_SAFE_INTEGER;
                EPOCH_MAX = Number.MIN_SAFE_INTEGER;

                TICKS.forEach((tick, i) => {
                    if (tick.Count == 0) {
                        return;
                    }

                    EPOCH_MIN = Math.min(tick.EpochStart, EPOCH_MIN);
                    EPOCH_MAX = Math.max(tick.EpochEnd, EPOCH_MAX);

                    const $tick = document.createElement('div');
                    $tick.classList.add('tick');
                    $tick.style.left = `${tick.Left}px`;
                    $tick.style.setProperty('--height', `${tick.Count / max * 100}%`);

                    const $content = document.createElement('div');
                    $content.classList.add('content');
                    $tick.appendChild($content);

                    ['Error', 'Warn', 'Info', 'Debug'].forEach(key => {
                        let count = tick[key];
                        if (count == 0) {
                            return
                        }
                        const $detail = document.createElement('div');
                        $detail.classList.add(key.toLowerCase());
                        $detail.style.flex = `${count}`;
                        $content.appendChild($detail);
                    })

                    $tick.onclick = () => {
                        EPOCH_DESIRED = tick.EpochStart + Math.ceil((tick.EpochEnd - tick.EpochStart) / 2);
                        clearEntries();
                        loadEntries('after');
                        loadEntries('before');
                        EPOCH_DESIRED = null;
                    }

                    $tick.onmouseover = () => {
                        highlightTick(tick);
                    }

                    $bars.appendChild($tick);
                    tick.Element = $tick;
                })

                // date marks
                const $dates = $chart.querySelector(".dates");
                $dates.textContent = '';

                let format = ({
                    s: 'MM-DD HH:mm:ss',
                    m: 'MM-DD HH:mm:ss',
                    h: 'MM-DD HH:mm',
                    d: 'YYYY-MM-DD HH:mm',
                })[INTERVAL_UNIT];


                let pcts = [5, 15, 25, 35, 50, 65, 75, 85, 95];
                if (window.outerWidth < 768) {
                    pcts = [5, 50, 95];
                }

                pcts.forEach(percent => {
                    let tickIndex = Math.floor((TICKS.length - 1) * percent / 100);
                    let tick = TICKS[tickIndex];

                    const $div = document.createElement('div');
                    $div.innerHTML = tick.Date.format(format);
                    $div.style.left = `${tick.Left}px`;
                    $dates.appendChild($div);
                });

                loadEntries('after');
                loadEntries('before');
            })
            .catch(console.error);
    }

    function updateEntriesTick(entries) {
        entries.forEach(entry => {
            for (let index = 0; index < TICKS.length; index++) {
                const tick = TICKS[index];
                // Epoch
                // EpochEnd EpochStart
                if (entry.Epoch >= tick.EpochStart && entry.Epoch <= tick.EpochEnd) {
                    entry.Tick = tick;
                    break
                }
            }
        });
    }

    /**
      * Loads the next set of records
      * 
      * @param {string} direction before|after
      */
    function loadEntries(direction) {

        if (direction == 'before') {
            if (IS_LOADING_BEFORE || !HAS_MORE_BEFORE) {
                return
            }
            IS_LOADING_BEFORE = true;
        } else {
            if (IS_LOADING_AFTER || !HAS_MORE_AFTER) {
                return
            }
            IS_LOADING_AFTER = true;
        }

        let epoch = EPOCH_END;
        if (EPOCH_DESIRED) {
            epoch = EPOCH_DESIRED;
        }
        let nanos = 0;

        if (ENTRIES.length > 0) {
            let entry;
            if (direction == 'before') {
                entry = ENTRIES[ENTRIES.length - 1];
            } else {
                entry = ENTRIES[0];
            }
            epoch = entry.Epoch;
            nanos = entry.Nanos;
        }

        const url = "./api/entries?" + new URLSearchParams({
            "expr": expression,
            "dir": direction,
            "epoch": epoch,
            "nanos": nanos,
            "limit": PAGE_SIZE,
        }).toString();

        const filterId = FILTER_ID;

        fetch(url)
            .then(data => data.json())
            .then((result) => {
                if (filterId != FILTER_ID) {
                    return
                }
                if (!result.entries || result.entries.length == 0) {
                    if (direction == 'before') {
                        HAS_MORE_BEFORE = false;
                    } else {
                        HAS_MORE_AFTER = false;
                    }
                    updateEntriesTick(ENTRIES);
                    return
                }

                let entries = result.entries.map((it) => {
                    let data = JSON.parse(it[3])
                    let level = it[2];
                    if (level < 0) {
                        level = 'DEBUG';
                    } else if (level < 4) {
                        level = 'INFO';
                    } else if (level < 8) {
                        level = 'WARN';
                    } else {
                        level = 'ERROR';
                    }

                    let entry = {
                        Epoch: it[0],
                        Nanos: it[1],
                        Message: data.msg,
                        Level: level,
                        Date: moment(new Date(it[0] * 1000 + it[1] / 1000000)),
                        Data: data,
                        Element: null,
                        Overview: getTags(data)
                    }

                    return entry
                });

                // EPOCH_END = end.unix();
                // EPOCH_START = start.unix();

                if (direction == 'before') {
                    // DESC
                    entries = entries.sort((entry, b) => {
                        if (entry.Epoch >= EPOCH_START) {
                            return true
                        }
                        HAS_MORE_BEFORE = false;
                        return false
                    }).sort((a, b) => {
                        return a.Date.isBefore(b.Date) ? 1 : -1;
                    });
                    ENTRIES.push(...entries)
                } else {
                    // ASC
                    entries = entries.sort((entry, b) => {
                        if (entry.Epoch <= EPOCH_END) {
                            return true
                        }
                        HAS_MORE_AFTER = false;
                        return false
                    }).sort((a, b) => {
                        return a.Date.isBefore(b.Date) ? -1 : 1;
                    });
                    ENTRIES.unshift(...entries);
                }

                updateEntriesTick(ENTRIES);
                renderEntries(entries, direction);
            })
            .catch(console.error)
            .finally(() => {
                if (filterId != FILTER_ID) {
                    return
                }

                if (direction == 'before') {
                    IS_LOADING_BEFORE = false;
                } else {
                    IS_LOADING_AFTER = false;
                }
                checkScroll()
            });
    }

    function renderEntries(entries, direction) {
        const tbody = $('#tab-content > table tbody');
        const rowTemplate = document.querySelector("#tpl-tab-row").content;

        let trs = entries.map(entry => {
            const tr = rowTemplate.cloneNode(true).querySelector('tr');
            tr.id = `_${entry.Epoc}_${entry.Nanos}`; // to hide while filtering
            tr.classList.add(entry.Level.toLowerCase());

            const tds = tr.querySelectorAll("td");
            tds[0].textContent = entry.Level;
            tds[1].textContent = entry.Date.format('YY-MM-DD HH:mm:ss.SSS');
            tds[2].textContent = entry.Message;
            tds[3].innerHTML = entry.Overview;

            $('.tag', tds[3]).on('click', (e, el) => {
                console.log(e, el);
                return false
            })

            tr.onclick = () => {
                showEventAttributes(entry);
            }

            tr.onmouseover = () => {
                highlightTick(entry.Tick, entry);
            }

            entry.Element = tr;

            return tr
        });

        if (direction == 'before') {
            trs.forEach((tr) => {
                tbody.append(tr);
            });

            // remove from the beginning
            if (!IS_LOADING_AFTER && ENTRIES.length > 60) {
                HAS_MORE_AFTER = true;
                let toRemove = ENTRIES.splice(0, ENTRIES.length - 60);

                queueMicrotask(() => {
                    let viewTop = $container.scrollTop();
                    let heightBefore = $content.height();

                    toRemove.forEach(entry => {
                        entry.Element.parentNode.removeChild(entry.Element);
                    });

                    let chunkHeight = $content.height() - heightBefore;
                    $container.scrollTop(viewTop + chunkHeight);
                });
            }

        } else {
            let viewTop = $container.scrollTop();
            let heightBefore = $content.height();

            trs.reverse().forEach((tr) => {
                tbody.prepend(tr);
            });

            let chunkHeight = $content.height() - heightBefore;
            $container.scrollTop(viewTop + chunkHeight);

            // remove items from the end
            if (!IS_LOADING_BEFORE && ENTRIES.length > 60) {
                HAS_MORE_BEFORE = true
                let toRemove = ENTRIES.splice(60);
                queueMicrotask(() => {
                    toRemove.forEach(entry => {
                        entry.Element.parentNode.removeChild(entry.Element);
                    });
                });
            }
        }
    }

    function getTags(json) {
        let out = [];
        for (const [key, value] of Object.entries(json)) {
            if (key == 'level' || key == 'time' || key == 'msg' || (typeof (value) == 'object')) {
                continue
            }
            out.push(`<span class="tag" title="Add to filter"><span class="key">${key}</span> <span class="value">${value}</span></span>`);
        }
        return out.join(' ');
    }

    let currentHighlight;
    function highlightTick(tick, entry) {
        if (tick == currentHighlight && !entry) {
            return
        }
        if (currentHighlight) {
            currentHighlight.Element.classList.remove('highlight');
        }

        const $highlightDate = document.getElementById('highlight-date');

        if (tick) {

            tick.Element.classList.add('highlight');

            $highlightDate.classList.remove('hidden');
            $highlightDate.classList.remove('center');
            $highlightDate.classList.remove('left');
            $highlightDate.classList.remove('right');

            let format = ({
                s: 'MM-DD HH:mm:ss',
                m: 'MM-DD HH:mm:ss',
                h: 'MM-DD HH:mm',
                d: 'YYYY-MM-DD HH:mm',
            })[INTERVAL_UNIT];

            let date = tick.Date;
            let text;
            if (entry) {
                date = entry.Date;
                text = `${date.format(format)}`;
            } else {
                text = `${INTERVAL_STR} @ ${date.format(format)}`;
            }


            $highlightDate.textContent = text;

            // 8h @ 5/12 20:00
            let left = Math.floor(tick.Element.offsetLeft - ($highlightDate.clientWidth / 2) + ((TICK_WIDTH_PX + TICK_GAP_PX) / 2));
            if (left < 0) {
                left = Math.floor(tick.Element.offsetLeft);
                $highlightDate.classList.add('left');
            } else if ((left + $highlightDate.clientWidth) > document.body.clientWidth) {
                left = Math.floor(tick.Element.offsetLeft - $highlightDate.clientWidth + (TICK_WIDTH_PX + TICK_GAP_PX));
                $highlightDate.classList.add('right');
            } else {
                $highlightDate.classList.add('center');
            }
            if (entry) {
                let percent = Math.min(Math.max(((entry.Epoch - tick.EpochStart) / (tick.EpochEnd - tick.EpochStart)), 0), 1);
                left = left - TICK_WIDTH_PX / 2 - 1 + (percent * TICK_WIDTH_PX);
            }

            $highlightDate.style.left = `${left}px`;
        } else {
            $highlightDate.classList.add('hidden');
        }

        currentHighlight = tick;
    }



    let overlayClickHandler;

    function showEventAttributes(entry) {
        let $panel = document.getElementById('event-attributes');

        $panel.classList.add('active');
        document.body.classList.add('no-scroll');


        let $overlay = $panel.querySelector('.overlay');

        if (overlayClickHandler) {
            $overlay.removeEventListener('click', overlayClickHandler);
        }

        overlayClickHandler = () => {
            $panel.classList.remove('active');
            document.body.classList.remove('no-scroll');

            $overlay.removeEventListener('click', overlayClickHandler);
            overlayClickHandler = undefined;
        }

        $overlay.addEventListener('click', overlayClickHandler);

        let info = `<strong>${entry.Level}</strong> - ${entry.Date.format('YY-MM-DD HH:mm:ss.SSS')}`;

        $panel.querySelector('.container .info').innerHTML = info;
        $panel.querySelector('.container .json').textContent = JSON.stringify(entry.Data, null, 3);
    }

    function map(in_min, in_max, out_min, out_max) {
        return (this - in_min) * (out_max - out_min) / (in_max - in_min) + out_min;
    }

    function debounce(func, timeout = 300) {
        let timer;
        return (...args) => {
            clearTimeout(timer);
            timer = setTimeout(() => { func.apply(this, args); }, timeout);
        };
    }
})()