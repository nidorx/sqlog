(function () {

    let Nodes;
    let userAgentData;
    const log = throttle(sendLog, 30);

    // From: https://codepen.io/tholman/pen/nqBvVo
    (function () {
        var lastTime = 0;
        var vendors = ['ms', 'moz', 'webkit', 'o'];
        for (var x = 0; x < vendors.length && !window.requestAnimationFrame; ++x) {
            window.requestAnimationFrame = window[vendors[x] + 'RequestAnimationFrame'];
            window.cancelAnimationFrame = window[vendors[x] + 'CancelAnimationFrame']
                || window[vendors[x] + 'CancelRequestAnimationFrame'];
        }

        if (!window.requestAnimationFrame)
            window.requestAnimationFrame = function (callback, element) {
                var currTime = new Date().getTime();
                var timeToCall = Math.max(0, 16 - (currTime - lastTime));
                var id = window.setTimeout(function () { callback(currTime + timeToCall); },
                    timeToCall);
                lastTime = currTime + timeToCall;
                return id;
            };

        if (!window.cancelAnimationFrame)
            window.cancelAnimationFrame = function (id) {
                clearTimeout(id);
            };

        screen.orientation.lock("landscape");
    }());

    Nodes = {

        // Settings
        density: 13,
        drawDistance: 30,
        baseRadius: 5,
        maxLineThickness: 0.8,
        reactionSensitivity: 8,
        lineThickness: 0.5,

        points: [],
        mouse: { x: -1000, y: -1000, down: false },

        animation: null,

        canvas: null,
        context: null,

        imageInput: null,
        bgImage: null,
        bgCanvas: null,
        bgContext: null,
        bgContextPixelData: null,

        init: function () {
            // Set up the visual canvas 
            this.canvas = document.getElementById('canvas');
            this.context = canvas.getContext('2d');
            this.context.globalCompositeOperation = "lighter";
            this.canvas.width = window.outerWidth;
            this.canvas.height = window.outerHeight;
            this.canvas.style.display = 'block'

            this.imageInput = document.createElement('input');
            this.imageInput.setAttribute('type', 'file');
            this.imageInput.style.visibility = 'hidden';
            this.imageInput.addEventListener('change', this.upload, false);
            document.body.appendChild(this.imageInput);

            this.canvas.addEventListener('mousemove', this.mouseMove, false);
            this.canvas.addEventListener('mousedown', this.mouseDown, false);
            this.canvas.addEventListener('mouseup', this.mouseUp, false);
            this.canvas.addEventListener('mouseout', this.mouseOut, false);

            this.canvas.addEventListener('touchmove', this.mouseMove, false);
            this.canvas.addEventListener('touchstart', this.mouseDown, false);
            this.canvas.addEventListener('touchend', (event) => {
                this.mouseUp(event);
                this.mouseOut(event);
            }, false);
            this.canvas.addEventListener('touchcancel', this.mouseOut, false);

            window.onresize = function (event) {
                Nodes.canvas.width = window.innerWidth;
                Nodes.canvas.height = window.innerHeight;
                Nodes.onWindowResize();
            }

            // Load initial input image
            this.loadData(getImageData());
        },

        preparePoints: function () {

            // Clear the current points
            this.points = [];

            var width, height, i, j;

            var colors = this.bgContextPixelData.data;

            for (i = 0; i < this.canvas.height; i += this.density) {

                for (j = 0; j < this.canvas.width; j += this.density) {

                    var pixelPosition = (j + i * this.bgContextPixelData.width) * 4;

                    // Dont use whiteish pixels
                    if (colors[pixelPosition] > 200 && (colors[pixelPosition + 1]) > 200 && (colors[pixelPosition + 2]) > 200 || colors[pixelPosition + 3] === 0) {
                        continue;
                    }

                    var color = 'rgba(' + colors[pixelPosition] + ',' + colors[pixelPosition + 1] + ',' + colors[pixelPosition + 2] + ',' + '1)';
                    this.points.push({ x: j, y: i, originalX: j, originalY: i, color: color });

                }
            }
        },

        updatePoints: function () {

            var i, currentPoint, theta, distance;

            for (i = 0; i < this.points.length; i++) {

                currentPoint = this.points[i];

                theta = Math.atan2(currentPoint.y - this.mouse.y, currentPoint.x - this.mouse.x);

                if (this.mouse.down) {
                    distance = this.reactionSensitivity * 200 / Math.sqrt((this.mouse.x - currentPoint.x) * (this.mouse.x - currentPoint.x) +
                        (this.mouse.y - currentPoint.y) * (this.mouse.y - currentPoint.y));
                } else {
                    distance = this.reactionSensitivity * 100 / Math.sqrt((this.mouse.x - currentPoint.x) * (this.mouse.x - currentPoint.x) +
                        (this.mouse.y - currentPoint.y) * (this.mouse.y - currentPoint.y));
                }


                currentPoint.x += Math.cos(theta) * distance + (currentPoint.originalX - currentPoint.x) * 0.05;
                currentPoint.y += Math.sin(theta) * distance + (currentPoint.originalY - currentPoint.y) * 0.05;

            }
        },

        drawLines: function () {

            var i, j, currentPoint, otherPoint, distance, lineThickness;

            for (i = 0; i < this.points.length; i++) {

                currentPoint = this.points[i];

                // Draw the dot.
                this.context.fillStyle = currentPoint.color;
                this.context.strokeStyle = currentPoint.color;

                for (j = 0; j < this.points.length; j++) {

                    // Distaqnce between two points.
                    otherPoint = this.points[j];

                    if (otherPoint == currentPoint) {
                        continue;
                    }

                    distance = Math.sqrt((otherPoint.x - currentPoint.x) * (otherPoint.x - currentPoint.x) +
                        (otherPoint.y - currentPoint.y) * (otherPoint.y - currentPoint.y));

                    if (distance <= this.drawDistance) {

                        this.context.lineWidth = (1 - (distance / this.drawDistance)) * this.maxLineThickness * this.lineThickness;
                        this.context.beginPath();
                        this.context.moveTo(currentPoint.x, currentPoint.y);
                        this.context.lineTo(otherPoint.x, otherPoint.y);
                        this.context.stroke();
                    }
                }
            }
        },

        drawPoints: function () {

            var i, currentPoint;

            for (i = 0; i < this.points.length; i++) {

                currentPoint = this.points[i];

                // Draw the dot.
                this.context.fillStyle = currentPoint.color;
                this.context.strokeStyle = currentPoint.color;

                this.context.beginPath();
                this.context.arc(currentPoint.x, currentPoint.y, this.baseRadius, 0, Math.PI * 2, true);
                this.context.closePath();
                this.context.fill();
            }
        },

        draw: function () {
            this.animation = requestAnimationFrame(function () { Nodes.draw() });

            this.clear();
            this.updatePoints();
            this.drawLines();
            this.drawPoints();
        },

        clear: function () {
            this.canvas.width = this.canvas.width;
        },

        // The filereader has loaded the image... add it to image object to be drawn
        loadData: function (data) {

            this.bgImage = new Image;
            this.bgImage.src = data;

            this.bgImage.onload = function () {
                Nodes.drawImageToBackground();
            }
        },

        // Image is loaded... draw to bg canvas
        drawImageToBackground: function () {

            this.bgCanvas = document.createElement('canvas');
            this.bgCanvas.width = this.canvas.width;
            this.bgCanvas.height = this.canvas.height;

            var newWidth, newHeight;

            // If the image is too big for the screen... scale it down.
            if (this.bgImage.width > this.bgCanvas.width - 30 || this.bgImage.height > this.bgCanvas.height - 30) {

                var maxRatio = Math.max(this.bgImage.width / (this.bgCanvas.width - 30), this.bgImage.height / (this.bgCanvas.height - 30));
                newWidth = this.bgImage.width / maxRatio;
                newHeight = this.bgImage.height / maxRatio;

            } else {
                newWidth = this.bgImage.width;
                newHeight = this.bgImage.height;
            }

            // Draw to background canvas
            this.bgContext = this.bgCanvas.getContext('2d');
            this.bgContext.drawImage(this.bgImage, (this.canvas.width - newWidth) / 2, (this.canvas.height - newHeight) / 2, newWidth, newHeight);
            this.bgContextPixelData = this.bgContext.getImageData(0, 0, this.bgCanvas.width, this.bgCanvas.height);

            this.preparePoints();
            this.draw();
        },

        mouseDown: function (event) {
            Nodes.mouse.down = true;
            log("mouse down");
        },

        mouseUp: function (event) {
            Nodes.mouse.down = false;
            log("mouse up");
        },

        mouseMove: function (event) {
            if (event.changedTouches) {
                var rect = event.target.getBoundingClientRect();
                var bodyRect = document.body.getBoundingClientRect();
                Nodes.mouse.x = event.changedTouches[0].pageX - (rect.left - bodyRect.left);
                Nodes.mouse.y = event.changedTouches[0].pageY - (rect.top - bodyRect.top);
            } else {
                Nodes.mouse.x = event.offsetX || (event.layerX - Nodes.canvas.offsetLeft);
                Nodes.mouse.y = event.offsetY || (event.layerY - Nodes.canvas.offsetTop);
            }

            log("mouse move");
        },

        mouseOut: function (event) {
            Nodes.mouse.x = -1000;
            Nodes.mouse.y = -1000;
            Nodes.mouse.down = false;
            log("mouse out");
        },

        // Resize and redraw the canvas.
        onWindowResize: function () {
            cancelAnimationFrame(this.animation);
            this.drawImageToBackground();
            log("window resized");
        },

        updateConfig: function () {
            cancelAnimationFrame(this.animation);
            this.preparePoints();
            this.draw();
        }
    }


    setTimeout(function () {
        Nodes.init();

        const gui = new lil.GUI();
        gui.add(Nodes, 'density', 9, 24, 1);
        gui.add(Nodes, 'drawDistance', 8, 40, 1);
        gui.add(Nodes, 'baseRadius', 1, 36, 1);
        gui.add(Nodes, 'maxLineThickness', 0.1, 8, 0.1);
        gui.add(Nodes, 'reactionSensitivity', 0.1, 10, 0.5);
        gui.add(Nodes, 'lineThickness', 0.1, 8, 0.5);

        gui.onChange(debounce(event => {
            Nodes.updateConfig();
            log("config updated")
        }, 100));

        try {
            navigator.userAgentData
                .getHighEntropyValues([
                    "architecture",
                    "bitness",
                    "model",
                    "platformVersion",
                    "uaFullVersion",
                    "fullVersionList",
                    "wow64",
                ])
                .then((ua) => {
                    userAgentData = ua;
                    log("captured navigator.userAgentData", "ERROR")
                })
                .catch(() => {
                    userAgentData = undefined;
                    log("error capturing navigator.userAgentData", "ERROR")
                });
        } catch {
            userAgentData = undefined;
            log("error capturing navigator.userAgentData", "ERROR")
        }
    }, 10);

    function sendLog(message, level, params) {
        params = {
            "msg": message,
            "level": level || (["DEBUG", "INFO", "WARN", "ERROR"])[Math.ceil(Math.random() * 3)],
            ...params,
            language: navigator.userLanguage || navigator.language,
            referrer: document.referrer,
            user_agent: navigator.userAgent
        };

        if (Nodes) {
            params = {
                ...params,
                points: Nodes.points.length,
                canvas_width: Nodes.canvas.width,
                canvas_height: Nodes.canvas.height,
                mouse_x: Nodes.mouse.x,
                mouse_y: Nodes.mouse.y,
                mouse_down: Nodes.mouse.down,
                density: Nodes.density,
                draw_distance: Nodes.drawDistance,
                base_radius: Nodes.baseRadius,
                line_thickness: Nodes.lineThickness,
                max_line_thickness: Nodes.maxLineThickness,
                reaction_sensitivity: Nodes.reactionSensitivity,
            }
        }

        if (userAgentData) {
            // User agent data returned by the Client Hints API
            params = {
                ...params,
                mobile: userAgentData.mobile,
                platform: userAgentData.platform,
                architecture: userAgentData.architecture,
                bitness: userAgentData.bitness,
                model: userAgentData.model,
                platform_version: userAgentData.platformVersion,
                wow64: userAgentData.wow64,
            }
        }

        fetch("/debug?" + new URLSearchParams(params).toString());
    }

    function debounce(func, timeout = 300) {
        let timer;
        return (...args) => {
            clearTimeout(timer);
            timer = setTimeout(() => { func.apply(this, args); }, timeout);
        };
    }

    function throttle(func, delay) {
        let timeout = null;
        return (...args) => {
            if (timeout) {
                return;
            }
            func(...args);
            timeout = setTimeout(() => {
                timeout = null;
            }, delay);
        };
    };

    function getImageData() {
        return 'data:image/gif;base64,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';
    }
})()