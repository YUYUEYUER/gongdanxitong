(function () {
    'use strict';

    var root = document.getElementById('csatWidget');
    var starsContainer = document.getElementById('stars');
    if (!root || !starsContainer) return;

    var uuid = root.dataset.csatUuid || '';
    if (!/^[0-9a-f-]{36}$/i.test(uuid)) return;

    var stars = Array.from(starsContainer.querySelectorAll('.star'));
    var done = false;
    function select (score) {
        stars.forEach(function (element, index) { element.classList.toggle('sel', index < score); });
    }

    stars.forEach(function (star) {
        star.addEventListener('mouseover', function (event) {
            if (!done) select(Number.parseInt(event.currentTarget.dataset.score, 10));
        });
        star.addEventListener('mouseout', function () { if (!done) select(0); });
        star.addEventListener('click', function (event) {
            event.preventDefault();
            if (done) return;
            var score = Number.parseInt(event.currentTarget.dataset.score, 10);
            if (!Number.isInteger(score) || score < 1 || score > 5) return;
            done = true;
            select(score);
            starsContainer.classList.add('disabled');
            document.getElementById('done').classList.add('visible');
            window.fetch('/api/v1/csat/' + encodeURIComponent(uuid) + '/response', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ rating: score }),
                credentials: 'omit'
            }).catch(function () { /* The user may retry from the full CSAT page. */ });
        });
    });
})();
