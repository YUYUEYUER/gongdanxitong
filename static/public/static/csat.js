(function () {
    'use strict';

    var form = document.querySelector('.csat-form');
    if (!form) return;

    var feedback = document.getElementById('feedback');
    var count = document.getElementById('charCount');
    var validation = document.getElementById('ratingValidationMessage');
    var button = document.getElementById('submitBtn');

    function updateCharCount () {
        count.textContent = String(feedback.value.length);
        count.parentElement.classList.toggle('warn', feedback.value.length > 900);
    }

    feedback.addEventListener('input', updateCharCount);
    form.addEventListener('submit', function (event) {
        var rating = document.querySelector('input[name="rating"]:checked');
        if (!rating) {
            event.preventDefault();
            validation.classList.add('show');
            window.setTimeout(function () { validation.classList.remove('show'); }, 4000);
            return;
        }
        validation.classList.remove('show');
        button.disabled = true;
        button.classList.add('is-loading');
    });

    document.querySelectorAll('input[name="rating"]').forEach(function (radio) {
        radio.addEventListener('change', function () { validation.classList.remove('show'); });
    });

    var initial = new URLSearchParams(window.location.search).get('rating');
    if (/^[1-5]$/.test(initial || '')) {
        var radio = document.getElementById('rating-' + initial);
        if (radio) radio.checked = true;
    }
    updateCharCount();
})();
