package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

// PokeStore handles with Poke, Record of Poke, arnd archived
type PokeStore interface {
	Create(c context.Context, p *Poke) (*Poke, error)
	Delete(c context.Context, IDs ...string) error
	Update(c context.Context, p *Poke) (*Poke, error)
	Get(c context.Context, IDs ...string) ([]*Poke, error)

	ListToSend(c context.Context) ([]*Poke, error)
	ListExpired(c context.Context) ([]*Poke, error)

	CreateRecord(c context.Context, r Record) (Record, error)
	GetRecord(c context.Context, messageID string) ([]*Record, error)

	Archive(c context.Context, id string) (*ArchivedPoke, error)
	DeleteArchived(c context.Context, IDs ...string) error
}

type firePokeStore struct {
	c          *firestore.Client
	pokeCol    *firestore.CollectionRef
	recCol     *firestore.CollectionRef
	archiveCol *firestore.CollectionRef
}

// firePokeStoreErr is an error
type firePokeStoreErr struct {
	storeErr error
	errFunc  string
	where    string
}

func (firePokeStoreErr) storeType() string { return "firepokestore" }
func (e firePokeStoreErr) Error() string {
	return fmt.Sprintf("%s %s: %v at %s", e.storeType(), e.errFunc, e.storeErr, e.where)
}

// NewFirePokeStore returns a firePokeStore, which is a PokeStore
func NewFirePokeStore(c *firestore.Client, pokeCol, recCol, arcCol string) (PokeStore, error) {
	if c == nil {
		return nil, firePokeStoreErr{
			fmt.Errorf("not created"),
			"newfirepokestore",
			"initialize",
		}
	}
	return &firePokeStore{
		c,
		c.Collection(pokeCol),
		c.Collection(recCol),
		c.Collection(arcCol),
	}, nil
}

// Create creates a Poke and gives it a ID
func (s *firePokeStore) Create(c context.Context, p *Poke) (*Poke, error) {
	docRef, _, err := s.pokeCol.Add(c, p)
	if err != nil {
		return nil, firePokeStoreErr{
			err,
			"create",
			p.ID,
		}
	}
	p.ID = docRef.ID
	return p, nil
}

// Delete deletes pokes with specified IDs. Mean to cancel a queuing poke
func (s *firePokeStore) Delete(ctx context.Context, IDs ...string) error {
	err := s.c.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		var err error
		for _, id := range IDs {
			err = tx.Delete(s.pokeCol.Doc(id))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return firePokeStoreErr{
			err,
			"delete",
			strings.Join(IDs, ","),
		}
	}
	return nil
}

// Update updates a existing poke.
func (s *firePokeStore) Update(ctx context.Context, p *Poke) (*Poke, error) {
	err := s.c.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.pokeCol.Doc(p.ID)
		// This error includes not found error
		if _, err := tx.Get(ref); err != nil {
			return err
		}
		return tx.Set(ref, p)
	})
	if err != nil {
		return nil, firePokeStoreErr{
			err,
			"update",
			p.ID,
		}
	}
	return p, nil
}

// Get returns []*Pokes
func (s *firePokeStore) Get(ctx context.Context, IDs ...string) ([]*Poke, error) {
	pokes := make([]*Poke, 0, len(IDs))
	for _, id := range IDs {
		d, err := s.pokeCol.Doc(id).Get(ctx)
		if err != nil {
			return nil, firePokeStoreErr{
				err,
				"get",
				strings.Join(IDs, ","),
			}
		}
		p := new(Poke)
		err = d.DataTo(p)
		if err != nil {
			return nil, firePokeStoreErr{
				err,
				"get",
				fmt.Sprintf("marshaling %s", id),
			}
		}
		p.ID = d.Ref.ID
		pokes = append(pokes, p)
	}
	return pokes, nil
}

// ListToSend lists all pokes that can be sent, includes expired ones.
func (s *firePokeStore) ListToSend(c context.Context) ([]*Poke, error) {
	q := s.pokeCol.Where("date_to_send", "<", time.Now())
	q = q.Limit(1000)
	
	docs, err := q.Documents(c).GetAll()
	if err != nil {
		return nil, firePokeStoreErr{
			err,
			"list_to_send",
			"",
		}
	}
	pokes := make([]*Poke, 0, len(docs))
	for _, doc := range docs {
		p := new(Poke)
		if err = doc.DataTo(p); err != nil {
			return nil, firePokeStoreErr{
				err,
				"list_to_send",
				doc.Ref.ID,
			}
		}
		p.ID = doc.Ref.ID
		pokes = append(pokes, p)
	}

	return pokes, nil
}

func (s *firePokeStore) ListExpired(c context.Context) ([]*Poke, error) {
	q := s.pokeCol.Where("expiry", "<", time.Now())
	q = q.Limit(1000)
	docs, err := q.Documents(c).GetAll()
	if err != nil {
		return nil, firePokeStoreErr{
			err,
			"list_expired",
			"",
		}
	}
	pokes := make([]*Poke, 0, len(docs))

	for _, d := range docs {
		p := new(Poke)
		if err := d.DataTo(p); err != nil {
			return nil, firePokeStoreErr{
				err,
				"list_expired",
				d.Ref.ID,
			}
		}
		p.ID = d.Ref.ID
		pokes = append(pokes, p)
	}
	return pokes, nil
}

func (s *firePokeStore) CreateRecord(ctx context.Context, r Record) (Record, error) {
	ref, _, err := s.recCol.Add(ctx, r)
	if err != nil {
		return Record{}, err
	}
	r.ID = ref.ID
	return r, nil
}

func (s *firePokeStore) GetRecord(ctx context.Context, messageID string) ([]*Record, error) {
	q := s.recCol.Where("message_id", "=", messageID)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, firePokeStoreErr{
			err,
			"GetRecord",
			messageID,
		}
	}
	r := make([]*Record, 0, len(docs))
	for _, d := range docs {
		rec := new(Record)
		err = d.DataTo(rec)
		if err != nil {
			return nil, firePokeStoreErr{
				err,
				"GetRecord",
				fmt.Sprintf("unmarshal message ID = %s", d.Ref.ID),
			}
		}
		rec.ID = d.Ref.ID
		r = append(r, rec)
	}
	return r, nil
}

// Archive moves a poke from queuing state to archived state.
// expired
func (s *firePokeStore) Archive(ctx context.Context, id string) (*ArchivedPoke, error) {
	pokeRef := s.pokeCol.Doc(id)
	arcRef := s.archiveCol.Doc(id)
	a := new(ArchivedPoke)
	t := time.Now()

	err := s.c.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		p := new(Poke)
		psnap, err := tx.Get(pokeRef)
		if err != nil {
			return err
		}
		err = psnap.DataTo(p)
		if err != nil {
			return err
		}
		p.ID = psnap.Ref.ID

		a = &ArchivedPoke{
			Tunnel:  p.Tunnel,
			To:      p.To,
			Expired: t.After(p.Expiry),
		}
		err = tx.Create(arcRef, a)
		if err != nil {
			return err
		}
		return tx.Delete(pokeRef)
	})
	if err != nil {
		return nil, firePokeStoreErr{
			err,
			"archive",
			id,
		}
	}
	return a, nil
}

func (s *firePokeStore) DeleteArchived(ctx context.Context, IDs ...string) error {
	refs := make([]*firestore.DocumentRef, 0, len(IDs))
	for _, id := range IDs {
		refs = append(refs, s.archiveCol.Doc(id))
	}

	err := s.c.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		var err error
		for _, ref := range refs {
			err = tx.Delete(ref)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return firePokeStoreErr{
			err,
			"delete archived",
			strings.Join(IDs, ","),
		}
	}
	return nil
}
