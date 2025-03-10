// FuryMesh Documentation JavaScript

document.addEventListener('DOMContentLoaded', function() {
  // Add scroll to top button
  const scrollToTopButton = document.createElement('button');
  scrollToTopButton.className = 'scroll-to-top';
  scrollToTopButton.innerHTML = '<i class="fas fa-arrow-up"></i>';
  scrollToTopButton.setAttribute('aria-label', 'Scroll to top');
  scrollToTopButton.setAttribute('title', 'Scroll to top');
  document.body.appendChild(scrollToTopButton);

  // Show/hide scroll to top button
  window.addEventListener('scroll', function() {
    if (window.pageYOffset > 300) {
      scrollToTopButton.classList.add('visible');
    } else {
      scrollToTopButton.classList.remove('visible');
    }
  });

  // Scroll to top when button is clicked
  scrollToTopButton.addEventListener('click', function() {
    window.scrollTo({
      top: 0,
      behavior: 'smooth'
    });
  });

  // Add copy buttons to code blocks
  document.querySelectorAll('pre > code').forEach(function(codeBlock) {
    const container = codeBlock.parentNode;
    const copyButton = document.createElement('button');
    copyButton.className = 'copy-button';
    copyButton.textContent = 'Copy';
    
    copyButton.addEventListener('click', function() {
      navigator.clipboard.writeText(codeBlock.textContent).then(function() {
        copyButton.textContent = 'Copied!';
        setTimeout(function() {
          copyButton.textContent = 'Copy';
        }, 2000);
      }, function(err) {
        console.error('Could not copy text: ', err);
        copyButton.textContent = 'Error!';
        setTimeout(function() {
          copyButton.textContent = 'Copy';
        }, 2000);
      });
    });
    
    container.appendChild(copyButton);
  });

  // Add search suggestions
  const searchInput = document.querySelector('.md-search__input');
  if (searchInput) {
    const suggestions = [
      'installation',
      'multi-peer transfers',
      'resume support',
      'encryption',
      'dht',
      'webrtc',
      'api',
      'configuration'
    ];
    
    const suggestionsContainer = document.createElement('div');
    suggestionsContainer.className = 'search-suggestions';
    
    const suggestionsTitle = document.createElement('div');
    suggestionsTitle.className = 'search-suggestions__title';
    suggestionsTitle.textContent = 'Popular searches:';
    
    const suggestionsList = document.createElement('div');
    suggestionsList.className = 'search-suggestions__list';
    
    suggestions.forEach(function(suggestion) {
      const suggestionItem = document.createElement('button');
      suggestionItem.className = 'search-suggestions__item';
      suggestionItem.textContent = suggestion;
      suggestionItem.addEventListener('click', function() {
        searchInput.value = suggestion;
        searchInput.dispatchEvent(new Event('input'));
      });
      suggestionsList.appendChild(suggestionItem);
    });
    
    suggestionsContainer.appendChild(suggestionsTitle);
    suggestionsContainer.appendChild(suggestionsList);
    
    const searchModal = document.querySelector('.md-search__inner');
    if (searchModal) {
      searchModal.insertBefore(suggestionsContainer, searchModal.firstChild.nextSibling);
    }
  }

  // Add custom footer links
  const footer = document.querySelector('.md-footer-meta__inner');
  if (footer) {
    const customFooter = document.createElement('div');
    customFooter.className = 'md-footer-custom';
    
    const links = document.createElement('div');
    links.className = 'footer-links';
    links.innerHTML = `
      <a href="https://github.com/TFMV/furymesh">GitHub</a>
      <span class="footer-link-divider">|</span>
      <a href="https://github.com/TFMV/furymesh/issues">Issues</a>
      <span class="footer-link-divider">|</span>
      <a href="https://github.com/TFMV/furymesh/releases">Releases</a>
      <span class="footer-link-divider">|</span>
      <a href="https://github.com/TFMV/furymesh/blob/main/LICENSE">License</a>
    `;
    
    const tagline = document.createElement('div');
    tagline.className = 'footer-tagline';
    tagline.textContent = 'FuryMesh - Decentralized peer-to-peer file sharing';
    
    customFooter.appendChild(links);
    customFooter.appendChild(tagline);
    
    footer.appendChild(customFooter);
  }

  // Add version selector
  const headerRight = document.querySelector('.md-header__source');
  if (headerRight) {
    const versionSelect = document.createElement('div');
    versionSelect.className = 'md-header__version-select';
    
    const version = document.createElement('div');
    version.className = 'md-version';
    
    const current = document.createElement('button');
    current.className = 'md-version__current';
    current.textContent = 'v0.1.0';
    
    const list = document.createElement('ul');
    list.className = 'md-version__list';
    
    const versions = [
      { version: 'v0.1.0', url: '#', active: true },
      { version: 'latest', url: '#', active: false }
    ];
    
    versions.forEach(function(v) {
      const item = document.createElement('li');
      item.className = 'md-version__item';
      
      const link = document.createElement('a');
      link.className = 'md-version__link' + (v.active ? ' md-version__link--active' : '');
      link.href = v.url;
      link.textContent = v.version;
      
      item.appendChild(link);
      list.appendChild(item);
    });
    
    version.appendChild(current);
    version.appendChild(list);
    versionSelect.appendChild(version);
    
    headerRight.parentNode.insertBefore(versionSelect, headerRight);
  }

  // Enhance mobile navigation
  enhanceMobileNavigation();
});

// Function to enhance mobile navigation
function enhanceMobileNavigation() {
  // Check if we're on mobile
  const isMobile = window.matchMedia("(max-width: 76.1875em)").matches;
  
  if (isMobile) {
    // Ensure navigation is visible on mobile
    const primarySidebar = document.querySelector('.md-sidebar--primary');
    const secondarySidebar = document.querySelector('.md-sidebar--secondary');
    
    if (primarySidebar) {
      primarySidebar.style.display = 'block';
      primarySidebar.style.position = 'fixed';
      primarySidebar.style.zIndex = '21';
    }
    
    // Add a toggle button for mobile navigation if it doesn't exist
    if (!document.querySelector('.mobile-nav-toggle')) {
      const header = document.querySelector('.md-header__inner');
      if (header) {
        const toggleButton = document.createElement('button');
        toggleButton.className = 'mobile-nav-toggle';
        toggleButton.innerHTML = '<i class="fas fa-bars"></i>';
        toggleButton.setAttribute('aria-label', 'Toggle navigation');
        
        // Insert before the search button
        const searchButton = document.querySelector('.md-header__button.md-icon');
        if (searchButton) {
          header.insertBefore(toggleButton, searchButton);
        } else {
          header.appendChild(toggleButton);
        }
        
        // Toggle navigation when button is clicked
        toggleButton.addEventListener('click', function() {
          if (primarySidebar) {
            const isVisible = primarySidebar.classList.contains('md-sidebar--open');
            if (isVisible) {
              primarySidebar.classList.remove('md-sidebar--open');
              toggleButton.innerHTML = '<i class="fas fa-bars"></i>';
            } else {
              primarySidebar.classList.add('md-sidebar--open');
              toggleButton.innerHTML = '<i class="fas fa-times"></i>';
            }
          }
        });
        
        // Close navigation when clicking outside
        document.addEventListener('click', function(event) {
          if (primarySidebar && primarySidebar.classList.contains('md-sidebar--open')) {
            if (!primarySidebar.contains(event.target) && !toggleButton.contains(event.target)) {
              primarySidebar.classList.remove('md-sidebar--open');
              toggleButton.innerHTML = '<i class="fas fa-bars"></i>';
            }
          }
        });
      }
    }
  }
} 