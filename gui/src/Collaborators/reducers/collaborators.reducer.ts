/*
  Copyright (C) 2016 H2O.ai, Inc. <http://h2o.ai/>

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as
  published by the Free Software Foundation, either version 3 of the
  License, or (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

import * as _ from 'lodash';
import {
  RECEIVE_MEMBERS_FOR_PROJECT, RECEIVE_LABELS_FOR_PROJECT
} from '../actions/collaborators.actions';

let initialState = {
  members: [],
  labels: []
};

export const collaboratorsReducer = (state = initialState, action) => {
  switch (action.type) {
    case RECEIVE_MEMBERS_FOR_PROJECT :
      return _.assign({}, state, { members: action.members });
    case RECEIVE_LABELS_FOR_PROJECT :
      return _.assign({}, state, { labels: action.labels });
    default:
      return state;
  }
};
