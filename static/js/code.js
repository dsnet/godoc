function showHidden(id) {
	if (id.startsWith("example-")) {
		elem = document.getElementById(id);
		elem = elem.getElementsByClassName("example-body")[0];
		elem.style.display = 'block';
		return false;
	}
	return true;
}
function toggleHidden(id) {
	if (id.startsWith("example-")) {
		elem = document.getElementById(id);
		elem = elem.getElementsByClassName("example-body")[0];
		elem.style.display = elem.style.display === 'block' ? 'none' : 'block';
		return false;
	}
	return true;
}

// If the URL navigates to a particular anchor (e.g., example-Foo),
// then show that element explicitly.
window.onhashchange = function () {
	showHidden(window.location.hash.slice(1));
}
if (window.location.hash != "") {
	showHidden(window.location.hash.slice(1));
}

// Register an onclick callback to toggle hiding the example body.
for (i = 0; i < document.links.length; i++) {
	anchor = document.links[i];
	href = anchor.getAttribute("href");
	if (href.startsWith("#example-")) {
		anchor.onclick = function (href) {
			return function() {
				console.log("toggle", href)
				toggleHidden(href.slice(1));
			}
		}(href);
	}
}