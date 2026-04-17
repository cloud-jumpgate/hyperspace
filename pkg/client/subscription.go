package client

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/cloud-jumpgate/hyperspace/pkg/driver/conductor"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
)

// Subscription receives messages from a channel and stream.
// The caller drives delivery by calling Poll in a duty-cycle loop.
//
// Subscriptions maintain a list of Images — one per remote publisher
// (identified by sessionID). Images are discovered by reconciling the
// Conductor's SubscriptionState with the client's known image set.
//
// Poll and Images are safe to call concurrently from multiple goroutines.
type Subscription struct {
	subscriptionID int64
	channel        string
	streamID       int32
	client         *Client
	mu             sync.RWMutex
	images         []*Image
	closed         atomic.Bool
}

// newSubscription creates a Subscription. Called only by Client.handleSubscriptionReady.
func newSubscription(subscriptionID int64, channel string, streamID int32, client *Client) *Subscription {
	return &Subscription{
		subscriptionID: subscriptionID,
		channel:        channel,
		streamID:       streamID,
		client:         client,
	}
}

// Poll polls all known Images for new fragments and invokes handler for each.
// Also reconciles the image list with the Conductor's current subscription state,
// adding any new images that the Conductor has registered.
// Returns the total number of fragments processed across all images.
func (s *Subscription) Poll(handler FragmentHandler, fragmentLimit int) int {
	if s.closed.Load() {
		return 0
	}

	// Reconcile images from the conductor before polling.
	s.reconcileImages()

	s.mu.RLock()
	images := make([]*Image, len(s.images))
	copy(images, s.images)
	s.mu.RUnlock()

	total := 0
	remaining := fragmentLimit
	for _, img := range images {
		if img.IsClosed() {
			continue
		}
		limit := remaining
		if limit <= 0 {
			break
		}
		n := img.Poll(handler, limit)
		total += n
		remaining -= n
	}
	return total
}

// reconcileImages checks the Conductor's SubscriptionState and adds any Images
// that have not yet been registered with this Subscription. This is how new
// publisher streams become visible to the subscriber.
func (s *Subscription) reconcileImages() {
	if s.client.drv == nil {
		return
	}

	subs := s.client.drv.Conductor().Subscriptions()
	for _, sub := range subs {
		if sub.SubscriptionID != s.subscriptionID {
			continue
		}
		// sub.Images contains ImageState entries added by the receiver.
		// In the current implementation the conductor does not populate Images
		// automatically via the receiver — this will be wired in the receiver sprint.
		// For the integration test path, we wire images directly via AddImage.
		for _, imgState := range sub.Images {
			if !s.hasImage(imgState.SessionID) {
				img := newImage(imgState.SessionID, s.streamID, imgState.LogBuf)
				s.mu.Lock()
				s.images = append(s.images, img)
				s.mu.Unlock()
				slog.Info("subscription: new image",
					"subscription_id", s.subscriptionID,
					"session_id", imgState.SessionID,
					"stream_id", s.streamID,
				)
			}
		}
	}
}

// hasImage returns true if an Image with the given sessionID already exists.
// Caller must NOT hold s.mu.
func (s *Subscription) hasImage(sessionID int32) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, img := range s.images {
		if img.SessionID() == sessionID {
			return true
		}
	}
	return false
}

// AddImage registers a new Image with the subscription. This is called
// directly in the integration path to wire a publication's log buffer
// to the subscription for in-process testing (before Receiver integration
// is complete).
func (s *Subscription) AddImage(sessionID, streamID int32, lb *logbuffer.LogBuffer) error {
	if s.closed.Load() {
		return errors.New("subscription: cannot add image to closed subscription")
	}
	img := newImage(sessionID, streamID, lb)
	s.mu.Lock()
	s.images = append(s.images, img)
	s.mu.Unlock()
	slog.Info("subscription: image added",
		"subscription_id", s.subscriptionID,
		"session_id", sessionID,
		"stream_id", streamID,
	)
	return nil
}

// AddImageFromConductor wires a publication's log buffer into this subscription
// by looking up the matching conductor PublicationState. This is the integration
// path for embedded-driver tests.
func (s *Subscription) AddImageFromConductor(pub *Publication) error {
	return s.AddImage(pub.SessionID(), pub.StreamID(), pub.logBuf)
}

// Channel returns the channel URI string.
func (s *Subscription) Channel() string { return s.channel }

// StreamID returns the stream identifier.
func (s *Subscription) StreamID() int32 { return s.streamID }

// Images returns a snapshot of all current Images for this Subscription.
func (s *Subscription) Images() []*Image {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := make([]*Image, len(s.images))
	copy(snap, s.images)
	return snap
}

// IsClosed reports whether this Subscription has been closed.
func (s *Subscription) IsClosed() bool { return s.closed.Load() }

// Close releases the subscription and notifies the conductor.
// Safe to call multiple times; subsequent calls are no-ops.
func (s *Subscription) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	s.mu.Lock()
	for _, img := range s.images {
		img.close()
	}
	s.mu.Unlock()

	slog.Info("subscription: closing",
		"subscription_id", s.subscriptionID,
		"stream_id", s.streamID,
	)

	// Send CmdRemoveSubscription to the conductor.
	_ = s.client.removeSubscription(s)

	// Drain the conductor's ImageState entries for this subscription.
	s.drainConductorImages()

	return nil
}

// drainConductorImages removes Images from the Conductor's SubscriptionState
// when the Subscription is closed. This prevents the conductor from referencing
// log buffers that the client no longer tracks.
func (s *Subscription) drainConductorImages() {
	if s.client.drv == nil {
		return
	}
	subs := s.client.drv.Conductor().Subscriptions()
	for _, sub := range subs {
		if sub.SubscriptionID == s.subscriptionID {
			sub.Images = sub.Images[:0]
			return
		}
	}
}

// InjectImageState adds an ImageState directly to the Conductor's SubscriptionState.
// This is used by integration tests to wire a publication's log buffer into the
// subscription without a real network receiver path. The injected ImageState will
// be discovered by reconcileImages on the next Poll call.
func (s *Subscription) InjectImageState(sessionID, streamID int32, lb *logbuffer.LogBuffer) {
	if s.client.drv == nil {
		return
	}
	subs := s.client.drv.Conductor().Subscriptions()
	for _, sub := range subs {
		if sub.SubscriptionID == s.subscriptionID {
			// Check for duplicates.
			for _, img := range sub.Images {
				if img.SessionID == sessionID {
					return
				}
			}
			sub.Images = append(sub.Images, &conductor.ImageState{
				SessionID: sessionID,
				LogBuf:    lb,
			})
			return
		}
	}
}
